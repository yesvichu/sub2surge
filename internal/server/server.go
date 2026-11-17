// Package server exposes the conversion as an HTTP endpoint that Surge can
// consume either through a policy group's policy-path or as a profile.
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/yesvichu/sub2surge/internal/config"
	"github.com/yesvichu/sub2surge/internal/convert"
	"github.com/yesvichu/sub2surge/internal/model"
	"github.com/yesvichu/sub2surge/internal/source"
	"github.com/yesvichu/sub2surge/internal/surgefile"
	"github.com/yesvichu/sub2surge/internal/version"
)

// Version is reported in headers and logs.
const Version = version.Version

// Server serves converted profiles over HTTP, one route per subscription.
type Server struct {
	cfg         config.Config
	subs        map[string]config.Sub
	defaultName string
	converter   *convert.Service
	mux         *http.ServeMux
}

// New wires the routes.
func New(cfg config.Config) *Server {
	s := &Server{
		cfg:         cfg,
		subs:        make(map[string]config.Sub, len(cfg.Subscriptions)),
		defaultName: cfg.DefaultName(),
		converter:   convert.New(cfg.TTL(), cfg.DefaultUA),
	}
	for _, sub := range cfg.Subscriptions {
		s.subs[sub.Name] = sub
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /{$}", s.handleDefault)
	mux.HandleFunc("GET /sub", s.handleDefault)
	mux.HandleFunc("GET /sub/{name}", s.handleSubscription)
	mux.HandleFunc("GET /preview", s.handlePreviewDefault)
	mux.HandleFunc("GET /preview/{name}", s.handlePreview)
	mux.HandleFunc("GET /", http.NotFound)
	s.mux = mux
	return s
}

// DefaultName is the subscription served at "/" and "/sub".
func (s *Server) DefaultName() string {
	return s.defaultName
}

// Convert runs the full pipeline without HTTP, used by the -once CLI mode.
func (s *Server) Convert(ctx context.Context, name string, query map[string][]string) ([]string, []model.Skip, error) {
	sub, ok := s.subs[name]
	if !ok {
		return nil, nil, fmt.Errorf("unknown subscription %q", name)
	}
	result, prepErr := s.prepare(ctx, sub, query)
	if prepErr != nil {
		return nil, nil, errors.New(prepErr.msg)
	}
	return result.lines, result.skips, nil
}

// ConvertProfile renders a complete Surge .conf file, used by the -out CLI mode.
func (s *Server) ConvertProfile(ctx context.Context, name string, query map[string][]string, managedURL string, includeRules bool, groups string) (string, error) {
	sub, ok := s.subs[name]
	if !ok {
		return "", fmt.Errorf("unknown subscription %q", name)
	}
	result, prepErr := s.prepare(ctx, sub, query)
	if prepErr != nil {
		return "", errors.New(prepErr.msg)
	}
	if len(result.lines) == 0 {
		return "", errors.New("no Surge-compatible nodes")
	}
	selection := surgefile.ParseSelection(groups)
	extraGroups := []string(nil)
	if includeRules {
		extraGroups = surgefile.DefaultProxyGroupsFor(result.names, selection)
	}
	return surgefile.RenderProfile(result.lines, surgefile.ProfileOptions{
		ManagedConfigURL: managedURL,
		Title:            "Sub2Surge export",
		Rules:            surgefile.RulesFor(includeRules, selection),
		ExtraGroups:      extraGroups,
	}), nil
}

// Handler returns the middleware-wrapped handler.
func (s *Server) Handler() http.Handler {
	return recoverMiddleware(logMiddleware(s.mux))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"version": Version,
		"subs":    len(s.subs),
	})
}

func (s *Server) handleDefault(w http.ResponseWriter, r *http.Request) {
	s.serveSubscription(w, r, s.defaultName)
}

func (s *Server) handleSubscription(w http.ResponseWriter, r *http.Request) {
	s.serveSubscription(w, r, r.PathValue("name"))
}

func (s *Server) handlePreviewDefault(w http.ResponseWriter, r *http.Request) {
	s.servePreview(w, r, s.defaultName)
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	s.servePreview(w, r, r.PathValue("name"))
}

type prepared struct {
	sub      config.Sub
	proxies  []model.Proxy
	lines    []string
	names    []string
	skips    []model.Skip
	warnings []string
	cache    source.Result
	target   string
	sources  int
}

func (s *Server) serveSubscription(w http.ResponseWriter, r *http.Request, name string) {
	sub, ok := s.lookupAndAuthorize(w, r, name)
	if !ok {
		return
	}
	format, err := requestFormat(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := surgefile.ValidateSelection(r.URL.Query().Get("groups")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	query := r.URL.Query()
	result, prepErr := s.prepare(r.Context(), sub, query)
	if prepErr != nil {
		http.Error(w, prepErr.Error(), prepErr.code)
		return
	}
	if len(result.lines) == 0 {
		http.Error(w, noNodesMessage(result), http.StatusBadGateway)
		return
	}

	writeCommonHeaders(w, result)
	switch format {
	case "profile":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, renderProfile(r, result))
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, renderList(result))
	}
}

func (s *Server) servePreview(w http.ResponseWriter, r *http.Request, name string) {
	sub, ok := s.lookupAndAuthorize(w, r, name)
	if !ok {
		return
	}
	result, prepErr := s.prepare(r.Context(), sub, r.URL.Query())
	if prepErr != nil {
		http.Error(w, prepErr.Error(), prepErr.code)
		return
	}

	views := make([]proxyView, 0, len(result.proxies))
	for _, proxy := range result.proxies {
		views = append(views, newProxyView(proxy))
	}
	response := previewResponse{
		Subscription:     result.sub.Name,
		Target:           result.target,
		Count:            len(views),
		ConvertedCount:   len(result.lines),
		Sources:          result.sources,
		Proxies:          views,
		Skipped:          result.skips,
		Warnings:         result.warnings,
		Cache:            cacheState(result.cache),
		FetchedAt:        result.cache.FetchedAt,
		UnsupportedCount: len(result.skips),
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) lookupAndAuthorize(w http.ResponseWriter, r *http.Request, name string) (config.Sub, bool) {
	sub, ok := s.subs[name]
	if !ok {
		http.Error(w, "unknown subscription", http.StatusNotFound)
		return config.Sub{}, false
	}
	if !s.authorized(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="sub2surge"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return config.Sub{}, false
	}
	return sub, true
}

func (s *Server) authorized(r *http.Request) bool {
	if s.cfg.Token == "" {
		return true
	}
	provided := r.URL.Query().Get("token")
	if provided == "" {
		if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
			provided = strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		}
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(s.cfg.Token)) == 1
}

type httpError struct {
	code int
	msg  string
}

func (e *httpError) Error() string { return e.msg }

func (s *Server) prepare(ctx context.Context, sub config.Sub, query map[string][]string) (*prepared, *httpError) {
	target := strings.ToLower(strings.TrimSpace(firstQuery(query, "target")))
	if target != "" && !strings.HasPrefix(target, "surge") {
		return nil, &httpError{http.StatusBadRequest, "unsupported target: this build only produces Surge output"}
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 50*time.Second)
	defer cancel()

	ua := firstNonEmpty(firstQuery(query, "ua"), sub.UA)
	prefix := firstNonEmpty(firstQuery(query, "prefix"), sub.Prefix)
	filter := firstNonEmpty(firstQuery(query, "filter"), sub.Filter)
	exclude := firstNonEmpty(firstQuery(query, "exclude"), sub.Exclude)
	sortResults := sub.Sort || queryBool(query, "sort")
	insecure := sub.Insecure || queryBool(query, "insecure", "allowInsecure")
	udp := sub.UDP || queryBool(query, "udp")
	tfo := sub.TFO || queryBool(query, "tfo")
	noCache := queryBool(query, "noCache", "nocache")

	var (
		result *convert.Result
		err    error
	)
	if mergeURL := strings.TrimSpace(firstQuery(query, "url2")); mergeURL != "" {
		result, err = s.converter.RunMerge(fetchCtx, convert.MergeRequest{
			Sources: []convert.Source{
				{URL: sub.URL, Label: firstQuery(query, "label1"), UA: ua},
				{URL: mergeURL, Label: firstQuery(query, "label2"), UA: ua},
			},
			Prefix:   prefix,
			Filter:   filter,
			Exclude:  exclude,
			Rename:   query["rename"],
			Sort:     sortResults,
			Insecure: insecure,
			UDP:      udp,
			TFO:      tfo,
			NoCache:  noCache,
		})
	} else {
		result, err = s.converter.Run(fetchCtx, convert.Request{
			URL:      sub.URL,
			UA:       ua,
			Prefix:   prefix,
			Filter:   filter,
			Exclude:  exclude,
			Rename:   query["rename"],
			Sort:     sortResults,
			Insecure: insecure,
			UDP:      udp,
			TFO:      tfo,
			NoCache:  noCache,
		})
	}
	if err != nil {
		return nil, conversionHTTPError(sub.Name, err)
	}

	return &prepared{
		sub:      sub,
		proxies:  result.Proxies,
		lines:    result.Lines,
		names:    result.Names,
		skips:    result.Skipped,
		warnings: result.Warnings,
		cache:    result.Upstream,
		target:   "Surge",
		sources:  result.Sources,
	}, nil
}

func conversionHTTPError(subName string, err error) *httpError {
	var invalid *convert.InvalidRequestError
	if errors.As(err, &invalid) {
		return &httpError{http.StatusBadRequest, invalid.Error()}
	}
	var options *convert.OptionsError
	if errors.As(err, &options) {
		return &httpError{http.StatusBadRequest, options.Error()}
	}
	var upstream *convert.UpstreamError
	if errors.As(err, &upstream) {
		log.Printf("subscription %q: fetch failed: %v", subName, upstream.Err)
		return &httpError{http.StatusBadGateway, upstream.Error()}
	}
	var parseErr *convert.ParseError
	if errors.As(err, &parseErr) {
		log.Printf("subscription %q: parse failed: %v", subName, parseErr.Err)
		return &httpError{http.StatusBadGateway, parseErr.Error()}
	}
	return &httpError{http.StatusInternalServerError, err.Error()}
}

func requestFormat(r *http.Request) (string, error) {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format"))) {
	case "", "list":
		return "list", nil
	case "profile", "conf", "config":
		return "profile", nil
	default:
		return "", fmt.Errorf("unsupported format %q (use list or profile)", r.URL.Query().Get("format"))
	}
}

func renderList(result *prepared) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# sub2surge %s | target=Surge | %d proxies | %d skipped | %s\n",
		Version, len(result.lines), len(result.skips), time.Now().Format(time.RFC3339))
	if result.cache.Stale {
		b.WriteString("# note: upstream unreachable, serving stale cache\n")
	}
	for _, warning := range result.warnings {
		fmt.Fprintf(&b, "# note: %s\n", warning)
	}
	for _, line := range surgefile.SummarizeSkips(result.skips, 8) {
		fmt.Fprintf(&b, "# skipped: %s\n", line)
	}
	for _, line := range result.lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func renderProfile(r *http.Request, result *prepared) string {
	includeRules := defaultRulesEnabled(r)
	selection := surgefile.ParseSelection(r.URL.Query().Get("groups"))
	extraGroups := []string(nil)
	if includeRules {
		extraGroups = surgefile.DefaultProxyGroupsFor(result.names, selection)
	}
	return surgefile.RenderProfile(result.lines, surgefile.ProfileOptions{
		ManagedConfigURL: absoluteURL(r),
		Title:            "Sub2Surge profile",
		Rules:            surgefile.RulesFor(includeRules, selection),
		ExtraGroups:      extraGroups,
	})
}

func defaultRulesEnabled(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("rules"))) {
	case "none", "0", "false", "off":
		return false
	default:
		return true
	}
}

func noNodesMessage(result *prepared) string {
	var b strings.Builder
	b.WriteString("# sub2surge: no Surge-compatible nodes\n")
	for _, line := range surgefile.SummarizeSkips(result.skips, 8) {
		fmt.Fprintf(&b, "# skipped: %s\n", line)
	}
	b.WriteString("# Unsupported nodes are listed above; check the preview endpoint for details.\n")
	return b.String()
}

func writeCommonHeaders(w http.ResponseWriter, result *prepared) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Sub2Surge-Version", Version)
	w.Header().Set("X-Sub2Surge-Count", strconv.Itoa(len(result.lines)))
	w.Header().Set("X-Sub2Surge-Skipped", strconv.Itoa(len(result.skips)))
	w.Header().Set("X-Sub2Surge-Cache", cacheState(result.cache))
	w.Header().Set("X-Sub2Surge-Sources", strconv.Itoa(result.sources))
}

func cacheState(result source.Result) string {
	switch {
	case result.Stale:
		return "stale"
	case result.Cached:
		return "hit"
	default:
		return "miss"
	}
}

type proxyView struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Server      string   `json:"server"`
	Port        int      `json:"port"`
	Network     string   `json:"network,omitempty"`
	TLS         bool     `json:"tls"`
	Reality     bool     `json:"reality,omitempty"`
	SNI         string   `json:"sni,omitempty"`
	ALPN        []string `json:"alpn,omitempty"`
	Insecure    bool     `json:"insecure,omitempty"`
	Plugin      string   `json:"plugin,omitempty"`
	Ports       string   `json:"ports,omitempty"`
	HasUUID     bool     `json:"has_uuid,omitempty"`
	HasPassword bool     `json:"has_password,omitempty"`
}

func newProxyView(proxy model.Proxy) proxyView {
	return proxyView{
		Name:        proxy.Name,
		Type:        proxy.Type,
		Server:      proxy.Server,
		Port:        proxy.Port,
		Network:     proxy.Network,
		TLS:         proxy.TLS,
		Reality:     proxy.Reality,
		SNI:         proxy.SNI,
		ALPN:        proxy.ALPN,
		Insecure:    proxy.Insecure,
		Plugin:      proxy.Plugin,
		Ports:       proxy.Ports,
		HasUUID:     proxy.UUID != "",
		HasPassword: proxy.Password != "",
	}
}

type previewResponse struct {
	Subscription     string       `json:"subscription"`
	Target           string       `json:"target"`
	Count            int          `json:"count"`
	ConvertedCount   int          `json:"converted_count"`
	Sources          int          `json:"sources"`
	Proxies          []proxyView  `json:"proxies"`
	Skipped          []model.Skip `json:"skipped,omitempty"`
	Warnings         []string     `json:"warnings,omitempty"`
	Cache            string       `json:"cache"`
	FetchedAt        time.Time    `json:"fetched_at,omitzero"`
	UnsupportedCount int          `json:"unsupported_count"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func absoluteURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := r.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		scheme = strings.TrimSpace(first)
	}
	host := r.Host
	if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		host = strings.TrimSpace(first)
	}
	return scheme + "://" + host + r.URL.RequestURI()
}

func firstQuery(query map[string][]string, key string) string {
	if values := query[key]; len(values) > 0 {
		return values[0]
	}
	return ""
}

func queryBool(query map[string][]string, keys ...string) bool {
	for _, key := range keys {
		values, ok := query[key]
		if !ok || len(values) == 0 {
			continue
		}
		value := strings.TrimSpace(values[0])
		if value == "" {
			return true
		}
		switch strings.ToLower(value) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(body)
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		if recorder.status == 0 {
			recorder.status = http.StatusOK
		}
		// Never log the query string: subscription tokens may live there.
		log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, recorder.status, time.Since(start).Round(time.Millisecond))
	})
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if value := recover(); value != nil {
				log.Printf("panic serving %s: %v", r.URL.Path, value)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
