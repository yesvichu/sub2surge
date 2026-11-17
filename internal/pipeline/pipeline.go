// Package pipeline implements the processing stage between parsing and
// producing: filters, renames, de-duplication and name normalization. This is
// the equivalent of Sub-Store's "processors" (a deliberately small subset).
package pipeline

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/yesvichu/sub2surge/internal/model"
)

// Options are the per-request/processing knobs.
type Options struct {
	Prefix   string
	Filter   string
	Exclude  string
	Rename   []string // "regex=>replacement", applied in order
	Sort     bool
	Insecure bool
	UDP      bool
	TFO      bool
}

// Apply returns a processed copy of proxies plus human-readable warnings.
func Apply(proxies []model.Proxy, opts Options) ([]model.Proxy, []string, error) {
	filter, err := compileOptional(opts.Filter)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid filter regex: %w", err)
	}
	exclude, err := compileOptional(opts.Exclude)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid exclude regex: %w", err)
	}

	type renameRule struct {
		re          *regexp.Regexp
		replacement string
	}
	renameRules := make([]renameRule, 0, len(opts.Rename))
	for _, raw := range opts.Rename {
		pattern, replacement, ok := strings.Cut(raw, "=>")
		if !ok {
			return nil, nil, fmt.Errorf("invalid rename rule %q (want regex=>replacement)", raw)
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid rename regex %q: %w", pattern, err)
		}
		renameRules = append(renameRules, renameRule{re: re, replacement: replacement})
	}

	var warnings []string
	result := make([]model.Proxy, 0, len(proxies))

	for _, proxy := range proxies {
		if filter != nil && !filter.MatchString(proxy.Name) {
			continue
		}
		if exclude != nil && exclude.MatchString(proxy.Name) {
			continue
		}
		if opts.Prefix != "" {
			proxy.Name = opts.Prefix + proxy.Name
		}
		for _, rule := range renameRules {
			proxy.Name = rule.re.ReplaceAllString(proxy.Name, rule.replacement)
		}
		if opts.Insecure {
			proxy.Insecure = true
		}
		if opts.UDP {
			proxy.UDP = true
		}
		if opts.TFO {
			proxy.TFO = true
		}
		result = append(result, proxy)
	}

	beforeDedupe := len(result)
	result = dedupe(result)
	if removed := beforeDedupe - len(result); removed > 0 {
		warnings = append(warnings, fmt.Sprintf("%d duplicate node(s) removed", removed))
	}

	result = uniqueNames(result)
	if opts.Sort {
		slices.SortStableFunc(result, func(a, b model.Proxy) int {
			if c := cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
				return c
			}
			if c := cmp.Compare(a.Server, b.Server); c != 0 {
				return c
			}
			return cmp.Compare(a.Port, b.Port)
		})
	}

	return result, warnings, nil
}

func compileOptional(pattern string) (*regexp.Regexp, error) {
	if strings.TrimSpace(pattern) == "" {
		return nil, nil
	}
	return regexp.Compile(pattern)
}

func dedupe(proxies []model.Proxy) []model.Proxy {
	seen := make(map[string]bool, len(proxies))
	result := proxies[:0]
	for _, proxy := range proxies {
		key := strings.Join([]string{
			proxy.Type,
			strings.ToLower(proxy.Server),
			strconv.Itoa(proxy.Port),
			proxy.UUID,
			proxy.Password,
			proxy.Network,
			proxy.WSPath,
		}, "\x00")
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, proxy)
	}
	return result
}

func uniqueNames(proxies []model.Proxy) []model.Proxy {
	used := make(map[string]bool, len(proxies))
	for i := range proxies {
		base := cleanName(proxies[i].Name)
		if base == "" {
			base = fmt.Sprintf("node-%d", i+1)
		}
		name := base
		for suffix := 2; used[strings.ToLower(name)]; suffix++ {
			name = fmt.Sprintf("%s %d", base, suffix)
		}
		used[strings.ToLower(name)] = true
		proxies[i].Name = name
	}
	return proxies
}

// cleanName mirrors the producer's sanitisation so duplicate detection uses the
// final policy name. Keep the two in sync if Surge's rules ever change.
func cleanName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case '=', ',', '"', '\n', '\r', '\t':
			return -1
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	if strings.HasPrefix(name, "#") || strings.HasPrefix(name, "//") ||
		strings.HasPrefix(name, ";") || strings.HasPrefix(name, "[") {
		name = "_" + name
	}
	return name
}
