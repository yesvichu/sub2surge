// Command sub2surge converts a V2Ray subscription into a Surge profile, either
// once from the command line or continuously over HTTP.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/yesvichu/sub2surge/internal/config"
	"github.com/yesvichu/sub2surge/internal/server"
	"github.com/yesvichu/sub2surge/internal/surgefile"
)

func main() {
	var (
		configPath = flag.String("config", "", "path to config.json (env: SUB2SURGE_CONFIG)")
		listen     = flag.String("listen", "", "listen address, e.g. 127.0.0.1:8080 (env: SUB2SURGE_LISTEN)")
		upstream   = flag.String("url", "", "upstream v2ray subscription URL; overrides config subscriptions (env: SUB2SURGE_URL)")
		upstream2  = flag.String("url2", "", "second subscription URL; merges with -url (use with -out or -once)")
		label1     = flag.String("label1", "订阅 A", "source label for -url when merging")
		label2     = flag.String("label2", "订阅 B", "source label for -url2 when merging")
		userAgent  = flag.String("ua", "", "User-Agent used to fetch the upstream (env: SUB2SURGE_UA)")
		token      = flag.String("token", "", "optional bearer token required by /sub and /preview (env: SUB2SURGE_TOKEN)")
		cacheTTL   = flag.String("cache-ttl", "", "upstream cache TTL, e.g. 1h (env: SUB2SURGE_CACHE_TTL)")
		once       = flag.Bool("once", false, "convert once, print Surge proxy lines to stdout, then exit")
		out        = flag.String("out", "", "write a complete Surge .conf profile to this file (or - for stdout) and exit")
		managedURL = flag.String("managed-url", "", "with -out, add #!MANAGED-CONFIG <url> to the profile")
		rules      = flag.String("rules", "default", "with -out, include the default Surge rules (default or none)")
		groups     = flag.String("groups", "all", "with -out, rule groups to include: all, none, or comma separated IDs")
		version    = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *version {
		fmt.Println("sub2surge " + server.Version)
		return
	}

	cfg := config.Config{
		Listen:    "127.0.0.1:8080",
		DefaultUA: "v2rayN/6.31",
		CacheTTL:  "1h",
	}

	path := *configPath
	if path == "" {
		path = os.Getenv("SUB2SURGE_CONFIG")
	}
	if path != "" {
		loaded, err := config.LoadFile(path)
		if err != nil {
			log.Fatalf("config: %v", err)
		}
		cfg = loaded
	}

	// Environment overrides the file; explicit flags override the environment.
	cfg.Listen = envOr("SUB2SURGE_LISTEN", cfg.Listen)
	cfg.DefaultUA = envOr("SUB2SURGE_UA", cfg.DefaultUA)
	cfg.CacheTTL = envOr("SUB2SURGE_CACHE_TTL", cfg.CacheTTL)
	cfg.Token = envOr("SUB2SURGE_TOKEN", cfg.Token)

	set := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if set["listen"] {
		cfg.Listen = *listen
	}
	if set["ua"] {
		cfg.DefaultUA = *userAgent
	}
	if set["token"] {
		cfg.Token = *token
	}
	if set["cache-ttl"] {
		cfg.CacheTTL = *cacheTTL
	}

	if *upstream != "" {
		cfg.Subscriptions = []config.Sub{{Name: "default", URL: *upstream}}
	} else if value := os.Getenv("SUB2SURGE_URL"); value != "" {
		cfg.Subscriptions = []config.Sub{{Name: "default", URL: value}}
	}

	if len(cfg.Subscriptions) == 0 {
		fmt.Fprintln(os.Stderr, "usage: sub2surge -url 'https://example.com/sub?token=xxx'")
		fmt.Fprintln(os.Stderr, "   or: sub2surge -config config.json")
		fmt.Fprintln(os.Stderr, "run 'sub2surge -h' for all flags")
		os.Exit(2)
	}

	if err := cfg.Normalize(); err != nil {
		log.Fatalf("config: %v", err)
	}

	srv := server.New(cfg)
	ctx := context.Background()

	mergeQuery := map[string][]string{}
	if *upstream2 != "" {
		mergeQuery["url2"] = []string{*upstream2}
		mergeQuery["label1"] = []string{*label1}
		mergeQuery["label2"] = []string{*label2}
	}

	if *out != "" {
		var includeRules bool
		switch strings.ToLower(strings.TrimSpace(*rules)) {
		case "", "default", "on", "true", "1":
			includeRules = true
		case "none", "off", "false", "0":
			includeRules = false
		default:
			log.Fatalf("invalid -rules %q (use default or none)", *rules)
		}
		if err := surgefile.ValidateSelection(*groups); err != nil {
			log.Fatalf("-groups: %v", err)
		}
		content, err := srv.ConvertProfile(ctx, srv.DefaultName(), mergeQuery, *managedURL, includeRules, *groups)
		if err != nil {
			log.Fatalf("convert: %v", err)
		}
		if *out == "-" {
			fmt.Print(content)
			return
		}
		if err := os.WriteFile(*out, []byte(content), 0o644); err != nil {
			log.Fatalf("write %s: %v", *out, err)
		}
		log.Printf("wrote %s", *out)
		return
	}

	if *once {
		lines, skips, err := srv.Convert(ctx, srv.DefaultName(), mergeQuery)
		if err != nil {
			log.Fatalf("convert: %v", err)
		}
		if len(lines) == 0 {
			log.Fatalf("no convertible nodes (%d skipped)", len(skips))
		}
		for _, line := range lines {
			fmt.Println(line)
		}
		fmt.Fprintf(os.Stderr, "# sub2surge: %d converted, %d skipped\n", len(lines), len(skips))
		return
	}

	if cfg.Token == "" && !isLoopback(cfg.Listen) {
		log.Printf("WARNING: listening on %s without a token; anyone who can reach this port can read your subscription", cfg.Listen)
	}

	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-signals.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	base := displayBase(cfg.Listen)
	log.Printf("sub2surge %s listening on %s", server.Version, cfg.Listen)
	log.Printf("  subscription: %s/sub  (or /sub/{name})", base)
	log.Printf("  preview:      %s/preview  (JSON, no secrets)", base)
	log.Printf("  health:       %s/healthz", base)

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func displayBase(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}
