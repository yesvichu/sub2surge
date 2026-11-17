package convert

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixtureBody() string {
	lines := []string{
		`{"add":"vmess.example.com","port":443,"id":"uuid","aid":0,"scy":"auto","net":"ws","path":"/ws","host":"cdn.example.com","tls":"tls","sni":"cdn.example.com","ps":"HK"}`,
		"trojan://pass@trojan.example.com:443?sni=trojan.example.com#US",
		"vless://uuid@vless.example.com:443?security=reality&sni=www.microsoft.com&pbk=key&sid=0123&type=tcp&flow=xtls-rprx-vision#Reality",
		"ssr://" + base64.RawURLEncoding.EncodeToString([]byte("1.2.3.4:8388:auth:aes-256-cfb:tls1.2_ticket_auth:cGFzcw/?remarks=U1NS")),
	}
	raw := "vmess://" + base64.StdEncoding.EncodeToString([]byte(lines[0])) + "\n" + strings.Join(lines[1:], "\n")
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

func TestRunAndCache(t *testing.T) {
	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = io.WriteString(w, fixtureBody())
	}))
	defer upstream.Close()

	service := New(time.Hour, "test-ua")
	request := Request{URL: upstream.URL, UDP: true}

	first, err := service.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(first.Lines) != 3 {
		t.Fatalf("lines = %d, want 3: %+v", len(first.Lines), first.Skipped)
	}
	if len(first.Skipped) != 1 || first.Skipped[0].Scheme != "ssr" {
		t.Fatalf("skips = %+v", first.Skipped)
	}
	if first.Upstream.Cached {
		t.Fatal("first call should not be cached")
	}
	for _, line := range first.Lines {
		if !strings.Contains(line, "udp-relay=true") {
			t.Fatalf("udp flag missing: %s", line)
		}
	}

	second, err := service.Run(context.Background(), request)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if !second.Upstream.Cached {
		t.Fatal("second call should hit the cache")
	}
	if hits != 1 {
		t.Fatalf("upstream hits = %d, want 1", hits)
	}
}

func TestRunValidation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, fixtureBody())
	}))
	defer upstream.Close()

	service := New(0, "ua")
	cases := []struct {
		request Request
		check   func(error) bool
	}{
		{Request{}, func(err error) bool {
			var target *InvalidRequestError
			return errors.As(err, &target)
		}},
		{Request{URL: "ftp://example.com"}, func(err error) bool {
			var target *InvalidRequestError
			return errors.As(err, &target)
		}},
		{Request{URL: upstream.URL, Filter: "("}, func(err error) bool {
			var target *OptionsError
			return errors.As(err, &target)
		}},
	}
	for _, tc := range cases {
		if _, err := service.Run(context.Background(), tc.request); err == nil || !tc.check(err) {
			t.Fatalf("request %+v: unexpected error %v", tc.request, err)
		}
	}
}

func TestFormatError(t *testing.T) {
	if got := FormatError(&InvalidRequestError{Msg: "bad url"}); !strings.Contains(got, "订阅地址无效") {
		t.Fatalf("unexpected message: %s", got)
	}
}

func TestRunMergeLabelsAndDeduplicates(t *testing.T) {
	body := func(uri string) string {
		return base64.StdEncoding.EncodeToString([]byte(uri + "\ntrojan://shared@shared.example.com:443#Shared"))
	}
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body("trojan://a@a.example.com:443#NodeA"))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body("trojan://b@b.example.com:443#NodeB"))
	}))
	defer second.Close()

	service := New(time.Hour, "ua")
	result, err := service.RunMerge(context.Background(), MergeRequest{
		Sources: []Source{
			{URL: first.URL, Label: "A"},
			{URL: second.URL, Label: "B"},
		},
	})
	if err != nil {
		t.Fatalf("RunMerge: %v", err)
	}
	if result.Sources != 2 {
		t.Fatalf("sources = %d", result.Sources)
	}
	names := strings.Join(result.Names, "\n")
	for _, want := range []string{"A - NodeA", "A - Shared", "B - NodeB"} {
		if !strings.Contains(names, want) {
			t.Fatalf("names missing %q:\n%s", want, names)
		}
	}
	if strings.Contains(names, "B - Shared") {
		t.Fatalf("duplicate shared node should be removed:\n%s", names)
	}
	if len(result.Lines) != 3 {
		t.Fatalf("lines = %d, want 3:\n%s", len(result.Lines), names)
	}

	if _, err := service.RunMerge(context.Background(), MergeRequest{
		Sources: []Source{{URL: first.URL, Label: "same"}, {URL: second.URL, Label: "same"}},
	}); err == nil {
		t.Fatal("expected duplicate label error")
	}
}

func TestRunMergeSourceErrorIsLabeled(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, fixtureBody())
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer second.Close()

	service := New(0, "ua")
	_, err := service.RunMerge(context.Background(), MergeRequest{
		Sources: []Source{
			{URL: first.URL, Label: "A"},
			{URL: second.URL, Label: "B"},
		},
	})
	if err == nil {
		t.Fatal("expected merge error")
	}
	if !strings.Contains(err.Error(), "B") {
		t.Fatalf("error should name source B: %v", err)
	}
}
