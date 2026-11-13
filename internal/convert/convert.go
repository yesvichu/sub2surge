// Package convert wires the parse → process → produce pipeline together. It is
// the single implementation used by the HTTP server, the CLI and the desktop
// app, mirroring Sub-Store's core with a much smaller surface.
package convert

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/yesvichu/sub2surge/internal/model"
	"github.com/yesvichu/sub2surge/internal/parser"
	"github.com/yesvichu/sub2surge/internal/pipeline"
	"github.com/yesvichu/sub2surge/internal/producer"
	"github.com/yesvichu/sub2surge/internal/source"
)

// Request describes one conversion.
type Request struct {
	URL      string
	UA       string
	Prefix   string
	Filter   string
	Exclude  string
	Rename   []string
	Sort     bool
	Insecure bool
	UDP      bool
	TFO      bool
	NoCache  bool
}

// Source is one subscription in a merge.
type Source struct {
	URL   string
	Label string
	UA    string
}

// MergeRequest describes merging several subscriptions into one result.
type MergeRequest struct {
	Sources  []Source
	Prefix   string
	Filter   string
	Exclude  string
	Rename   []string
	Sort     bool
	Insecure bool
	UDP      bool
	TFO      bool
	NoCache  bool
}

// Result is the output of a conversion.
type Result struct {
	Proxies  []model.Proxy
	Lines    []string
	Names    []string
	Skipped  []model.Skip
	Warnings []string
	Upstream source.Result
	Sources  int
}

// InvalidRequestError means the caller supplied bad input (HTTP 400).
type InvalidRequestError struct{ Msg string }

func (e *InvalidRequestError) Error() string { return e.Msg }

// UpstreamError means the subscription could not be fetched (HTTP 502).
type UpstreamError struct{ Err error }

func (e *UpstreamError) Error() string { return "upstream fetch failed: " + e.Err.Error() }
func (e *UpstreamError) Unwrap() error { return e.Err }

// ParseError means the body was not a v2ray subscription (HTTP 502).
type ParseError struct{ Err error }

func (e *ParseError) Error() string { return "cannot parse upstream subscription: " + e.Err.Error() }
func (e *ParseError) Unwrap() error { return e.Err }

// OptionsError means a filter/rename expression was invalid (HTTP 400).
type OptionsError struct{ Err error }

func (e *OptionsError) Error() string { return e.Err.Error() }
func (e *OptionsError) Unwrap() error { return e.Err }

// Service converts subscriptions. It is safe for concurrent use and keeps the
// upstream cache between calls.
type Service struct {
	fetcher   *source.Fetcher
	defaultUA string
}

// New creates a conversion service. ttl is the upstream cache TTL; zero
// disables caching.
func New(ttl time.Duration, defaultUA string) *Service {
	return &Service{
		fetcher:   source.New(ttl),
		defaultUA: strings.TrimSpace(defaultUA),
	}
}

// Run fetches, parses and converts one subscription. A result with zero lines
// is returned without an error; callers decide whether that is a failure.
func (s *Service) Run(ctx context.Context, req Request) (*Result, error) {
	parsed, upstream, err := s.load(ctx, req.URL, req.UA, req.NoCache)
	if err != nil {
		return nil, err
	}

	proxies, warnings, err := pipeline.Apply(parsed.Proxies, pipeline.Options{
		Prefix:   req.Prefix,
		Filter:   req.Filter,
		Exclude:  req.Exclude,
		Rename:   req.Rename,
		Sort:     req.Sort,
		Insecure: req.Insecure,
		UDP:      req.UDP,
		TFO:      req.TFO,
	})
	if err != nil {
		return nil, &OptionsError{Err: err}
	}

	return produce(proxies, parsed.Skipped, append(parsed.Warnings, warnings...), upstream, 1), nil
}

// RunMerge fetches every source, labels each node with its source, then merges
// and de-duplicates the combined list.
func (s *Service) RunMerge(ctx context.Context, req MergeRequest) (*Result, error) {
	if len(req.Sources) < 2 {
		return nil, &InvalidRequestError{Msg: "at least two subscription sources are required"}
	}

	labels := make([]string, len(req.Sources))
	seenLabels := make(map[string]bool, len(req.Sources))
	for i, src := range req.Sources {
		label := strings.TrimSpace(src.Label)
		if label == "" {
			label = fmt.Sprintf("订阅 %d", i+1)
		}
		if seenLabels[label] {
			return nil, &InvalidRequestError{Msg: "source labels must be different"}
		}
		seenLabels[label] = true
		labels[i] = label
	}

	var (
		allProxies []model.Proxy
		allSkips   []model.Skip
		warnings   []string
		firstCache source.Result
	)

	for i, src := range req.Sources {
		label := labels[i]
		parsed, upstream, err := s.load(ctx, src.URL, src.UA, req.NoCache)
		if err != nil {
			return nil, labelError(label, err)
		}
		if i == 0 {
			firstCache = upstream
		}

		prefix := label + " - "
		if req.Prefix != "" {
			prefix = req.Prefix + prefix
		}

		proxies, sourceWarnings, err := pipeline.Apply(parsed.Proxies, pipeline.Options{
			Prefix:   prefix,
			Filter:   req.Filter,
			Exclude:  req.Exclude,
			Rename:   req.Rename,
			Insecure: req.Insecure,
			UDP:      req.UDP,
			TFO:      req.TFO,
		})
		if err != nil {
			return nil, &OptionsError{Err: err}
		}
		allProxies = append(allProxies, proxies...)

		for _, skip := range parsed.Skipped {
			if skip.Name != "" {
				skip.Name = label + " - " + skip.Name
			} else {
				skip.Name = label
			}
			allSkips = append(allSkips, skip)
		}
		for _, warning := range sourceWarnings {
			warnings = append(warnings, label+": "+warning)
		}
		for _, warning := range parsed.Warnings {
			warnings = append(warnings, label+": "+warning)
		}
	}

	merged, finalWarnings, err := pipeline.Apply(allProxies, pipeline.Options{Sort: req.Sort})
	if err != nil {
		return nil, &OptionsError{Err: err}
	}
	warnings = append(warnings, finalWarnings...)

	return produce(merged, allSkips, warnings, firstCache, len(req.Sources)), nil
}

func (s *Service) load(ctx context.Context, rawURL, ua string, noCache bool) (*model.Result, source.Result, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, source.Result{}, &InvalidRequestError{Msg: "subscription URL is required"}
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return nil, source.Result{}, &InvalidRequestError{Msg: "subscription URL must be an absolute http(s) URL"}
	}

	userAgent := strings.TrimSpace(ua)
	if userAgent == "" {
		userAgent = s.defaultUA
	}
	key := rawURL + "\x00" + userAgent

	upstream, err := s.fetcher.Fetch(ctx, key, rawURL, userAgent, noCache)
	if err != nil {
		return nil, source.Result{}, &UpstreamError{Err: err}
	}

	parsed, err := parser.Parse(upstream.Body)
	if err != nil {
		return nil, source.Result{}, &ParseError{Err: err}
	}
	return parsed, upstream, nil
}

func produce(proxies []model.Proxy, skipped []model.Skip, warnings []string, upstream source.Result, sources int) *Result {
	result := &Result{
		Proxies:  proxies,
		Skipped:  append([]model.Skip(nil), skipped...),
		Warnings: append([]string(nil), warnings...),
		Upstream: upstream,
		Sources:  sources,
	}

	for _, proxy := range proxies {
		line, err := producer.Surge(proxy)
		if err != nil {
			result.Skipped = append(result.Skipped, model.Skip{
				Scheme: proxy.Type,
				Name:   proxy.Name,
				Reason: err.Error(),
			})
			continue
		}
		result.Lines = append(result.Lines, line)
		result.Names = append(result.Names, proxy.Name)
	}
	return result
}

func labelError(label string, err error) error {
	var invalid *InvalidRequestError
	if errors.As(err, &invalid) {
		return &InvalidRequestError{Msg: label + ": " + invalid.Msg}
	}
	var upstream *UpstreamError
	if errors.As(err, &upstream) {
		return &UpstreamError{Err: fmt.Errorf("%s: %w", label, upstream.Err)}
	}
	var parseErr *ParseError
	if errors.As(err, &parseErr) {
		return &ParseError{Err: fmt.Errorf("%s: %w", label, parseErr.Err)}
	}
	return fmt.Errorf("%s: %w", label, err)
}

// FormatError renders an error for a UI without exposing URL credentials.
func FormatError(err error) string {
	if err == nil {
		return ""
	}
	var invalid *InvalidRequestError
	if errors.As(err, &invalid) {
		return "订阅地址无效：" + invalid.Msg
	}
	var upstream *UpstreamError
	if errors.As(err, &upstream) {
		return "无法获取订阅：" + upstream.Err.Error()
	}
	var parseErr *ParseError
	if errors.As(err, &parseErr) {
		return "订阅内容无法解析：" + parseErr.Err.Error()
	}
	var options *OptionsError
	if errors.As(err, &options) {
		return "处理选项无效：" + options.Err.Error()
	}
	return err.Error()
}
