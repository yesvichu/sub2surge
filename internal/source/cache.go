// Package source fetches upstream subscriptions, with a small in-memory cache
// and request coalescing. Sub-Store caches remote subscriptions for one hour by
// default; the same idea applies here so a Surge policy-path refresh does not
// hammer the provider.
package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const maxBodySize = 32 << 20 // 32 MiB

// Result describes one upstream body.
type Result struct {
	Body      []byte
	Cached    bool
	Stale     bool
	FetchedAt time.Time
}

type entry struct {
	body      []byte
	expires   time.Time
	fetchedAt time.Time
}

type call struct {
	done chan struct{}
	res  Result
	err  error
}

// Fetcher is safe for concurrent use.
type Fetcher struct {
	client *http.Client
	ttl    time.Duration

	mu       sync.Mutex
	entries  map[string]*entry
	inflight map[string]*call
}

// New creates a fetcher. A zero or negative ttl disables caching.
func New(ttl time.Duration) *Fetcher {
	if ttl <= 0 {
		ttl = 0
	}
	return &Fetcher{
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 6 {
					return errors.New("too many redirects")
				}
				return nil
			},
		},
		ttl:      ttl,
		entries:  make(map[string]*entry),
		inflight: make(map[string]*call),
	}
}

// Fetch returns the body for rawURL. key should uniquely identify the request
// (URL + User-Agent); it is also used as the cache key. noCache bypasses the
// read path but still refreshes the stored copy.
func (f *Fetcher) Fetch(ctx context.Context, key, rawURL, userAgent string, noCache bool) (Result, error) {
	if f.ttl > 0 && !noCache {
		f.mu.Lock()
		if item := f.entries[key]; item != nil && time.Now().Before(item.expires) {
			result := Result{Body: item.body, Cached: true, FetchedAt: item.fetchedAt}
			f.mu.Unlock()
			return result, nil
		}
		f.mu.Unlock()
	}

	f.mu.Lock()
	if pending := f.inflight[key]; pending != nil {
		f.mu.Unlock()
		select {
		case <-pending.done:
			return pending.res, pending.err
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}
	pending := &call{done: make(chan struct{})}
	f.inflight[key] = pending
	f.mu.Unlock()

	result, err := f.download(ctx, rawURL, userAgent)

	f.mu.Lock()
	if err == nil {
		if f.ttl > 0 {
			f.entries[key] = &entry{
				body:      result.Body,
				expires:   time.Now().Add(f.ttl),
				fetchedAt: result.FetchedAt,
			}
		}
	} else if item := f.entries[key]; item != nil {
		// Serving stale data beats failing a Surge refresh.
		result = Result{Body: item.body, Cached: true, Stale: true, FetchedAt: item.fetchedAt}
		err = nil
	}

	pending.res, pending.err = result, err
	delete(f.inflight, key)
	close(pending.done)
	f.mu.Unlock()

	return result, err
}

func (f *Fetcher) download(ctx context.Context, rawURL, userAgent string) (Result, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Result{}, fmt.Errorf("invalid upstream URL")
	}
	if userAgent != "" {
		request.Header.Set("User-Agent", userAgent)
	}
	request.Header.Set("Accept", "*/*")

	response, err := f.client.Do(request)
	if err != nil {
		return Result{}, sanitizedError(rawURL, err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Result{}, fmt.Errorf("upstream returned HTTP %d", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxBodySize+1))
	if err != nil {
		return Result{}, fmt.Errorf("read upstream body: %w", err)
	}
	if len(body) > maxBodySize {
		return Result{}, fmt.Errorf("upstream body exceeds %d MiB", maxBodySize>>20)
	}
	if len(body) == 0 {
		return Result{}, fmt.Errorf("upstream returned an empty body")
	}

	return Result{Body: body, FetchedAt: time.Now()}, nil
}

// sanitizedError keeps subscription tokens out of logs: *url.Error includes
// the full request URL, which often has a token in the query string.
func sanitizedError(rawURL string, err error) error {
	host := "upstream"
	if parsed, parseErr := url.Parse(rawURL); parseErr == nil && parsed.Host != "" {
		host = parsed.Host
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("request %s failed: %v", host, urlErr.Err)
	}
	return fmt.Errorf("request %s failed: %v", host, err)
}
