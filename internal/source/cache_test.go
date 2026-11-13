package source

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"net/http"
	"net/http/httptest"
)

func TestFetchCacheAndStale(t *testing.T) {
	var hits int32
	var fail int32
	body := "hello"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.Header.Get("User-Agent") != "test-ua" {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		if atomic.LoadInt32(&fail) == 1 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	fetcher := New(time.Hour)
	ctx := context.Background()

	first, err := fetcher.Fetch(ctx, "k", server.URL, "test-ua", false)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if first.Cached || first.Stale || string(first.Body) != body {
		t.Fatalf("unexpected first result: %+v", first)
	}

	second, err := fetcher.Fetch(ctx, "k", server.URL, "test-ua", false)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if !second.Cached || string(second.Body) != body {
		t.Fatalf("unexpected second result: %+v", second)
	}
	if hits != 1 {
		t.Fatalf("upstream hits = %d, want 1", hits)
	}

	// noCache refreshes.
	if _, err := fetcher.Fetch(ctx, "k", server.URL, "test-ua", true); err != nil {
		t.Fatalf("noCache fetch: %v", err)
	}
	if hits != 2 {
		t.Fatalf("upstream hits = %d, want 2", hits)
	}

	// Fail the upstream: the cached copy should be served as stale.
	atomic.StoreInt32(&fail, 1)
	stale, err := fetcher.Fetch(ctx, "k", server.URL, "test-ua", true)
	if err != nil {
		t.Fatalf("stale fetch: %v", err)
	}
	if !stale.Stale || string(stale.Body) != body {
		t.Fatalf("unexpected stale result: %+v", stale)
	}
}

func TestFetchErrorDoesNotLeakToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL + "/sub?token=super-secret"
	server.Close()

	fetcher := New(time.Hour)
	_, err := fetcher.Fetch(context.Background(), "k", url, "ua", false)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("error leaked token: %v", err)
	}
}
