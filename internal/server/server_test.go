package server

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yesvichu/sub2surge/internal/config"
)

const testVMess = `{"add":"vmess.example.com","port":443,"id":"11111111-1111-1111-1111-111111111111","aid":0,"scy":"auto","net":"ws","path":"/ws","host":"cdn.example.com","tls":"tls","sni":"cdn.example.com","ps":"HK VMess"}`

func testBody() string {
	lines := []string{
		"vmess://" + base64.StdEncoding.EncodeToString([]byte(testVMess)),
		"trojan://super-secret@trojan.example.com:443?sni=trojan.example.com#US Trojan",
		"vless://22222222-2222-2222-2222-222222222222@vless.example.com:443?security=tls&sni=vless.example.com#VLESS node",
		"ssr://" + base64.RawURLEncoding.EncodeToString([]byte("1.2.3.4:8388:auth_aes128_md5:aes-256-cfb:tls1.2_ticket_auth:cGFzcw/?remarks="+base64.RawURLEncoding.EncodeToString([]byte("SSR old")))),
	}
	return base64.StdEncoding.EncodeToString([]byte(strings.Join(lines, "\n")))
}

func newTestServer(t *testing.T, token string) (*httptest.Server, *int32) {
	t.Helper()

	var hits int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = io.WriteString(w, testBody())
	}))
	t.Cleanup(upstream.Close)

	cfg := config.Config{
		Listen:    "127.0.0.1:0",
		Token:     token,
		DefaultUA: "test-ua",
		CacheTTL:  "1h",
		Subscriptions: []config.Sub{
			{Name: "demo", URL: upstream.URL + "/sub?token=upstream-secret"},
		},
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}

	ts := httptest.NewServer(New(cfg).Handler())
	t.Cleanup(ts.Close)
	return ts, &hits
}

func TestSubscriptionAndCache(t *testing.T) {
	ts, hits := newTestServer(t, "")

	response, err := http.Get(ts.URL + "/sub")
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, body)
	}
	if got := response.Header.Get("X-Sub2surge-Count"); got != "3" {
		t.Fatalf("count header = %q, want 3", got)
	}
	if got := response.Header.Get("X-Sub2surge-Skipped"); got != "1" {
		t.Fatalf("skipped header = %q, want 1", got)
	}
	if !strings.Contains(body, "=vmess,") || !strings.Contains(body, "=trojan,") || !strings.Contains(body, "=vless,") {
		t.Fatalf("missing proxy lines: %s", body)
	}
	if strings.Contains(body, "=ssr,") {
		t.Fatalf("ssr line should not be emitted: %s", body)
	}
	if !strings.Contains(body, "# skipped: ssr x1") {
		t.Fatalf("missing skip summary: %s", body)
	}
	if *hits != 1 {
		t.Fatalf("upstream hits = %d, want 1", *hits)
	}

	// The second request must use the cache.
	response, err = http.Get(ts.URL + "/preview")
	if err != nil {
		t.Fatal(err)
	}
	previewBody := readAll(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d, body = %s", response.StatusCode, previewBody)
	}
	if *hits != 1 {
		t.Fatalf("upstream hits after preview = %d, want 1", *hits)
	}

	var preview previewResponse
	if err := json.Unmarshal([]byte(previewBody), &preview); err != nil {
		t.Fatalf("decode preview: %v\n%s", err, previewBody)
	}
	if preview.Count != 3 || preview.ConvertedCount != 3 || preview.UnsupportedCount != 1 {
		t.Fatalf("unexpected preview counts: %+v", preview)
	}
	if strings.Contains(previewBody, "super-secret") || strings.Contains(previewBody, "11111111-1111") {
		t.Fatalf("preview leaked credentials: %s", previewBody)
	}
}

func TestProfileOutput(t *testing.T) {
	ts, _ := newTestServer(t, "")
	response, err := http.Get(ts.URL + "/sub?format=profile")
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, body)
	}
	firstLine, _, _ := strings.Cut(body, "\n")
	if !strings.HasPrefix(firstLine, "#!MANAGED-CONFIG ") || !strings.Contains(firstLine, "format=profile") {
		t.Fatalf("unexpected managed-config line: %q", firstLine)
	}
	for _, want := range []string{"[Proxy]", "[Proxy Group]", "🔰国外流量 = select,", "[Rule]", "GEOIP,CN,🚀直接连接", "FINAL,⚓️其他流量", "⚓️其他流量 = select,"} {
		if !strings.Contains(body, want) {
			t.Fatalf("profile missing %q:\n%s", want, body)
		}
	}

	response, err = http.Get(ts.URL + "/sub?format=profile&rules=none")
	if err != nil {
		t.Fatal(err)
	}
	plain := readAll(t, response)
	if strings.Contains(plain, "GEOIP,CN") {
		t.Fatalf("rules=none should not include default rules:\n%s", plain)
	}
	if strings.Contains(plain, "⚓️其他流量") {
		t.Fatalf("rules=none should not include default groups:\n%s", plain)
	}
	if !strings.Contains(plain, "FINAL,DIRECT") {
		t.Fatalf("rules=none should keep FINAL,DIRECT:\n%s", plain)
	}

	response, err = http.Get(ts.URL + "/sub?format=profile&groups=cn-ai")
	if err != nil {
		t.Fatal(err)
	}
	onlyCN := readAll(t, response)
	for _, want := range []string{"DOMAIN-SUFFIX,deepseek.com,🇨🇳国产AI", "🇨🇳国产AI = select, DIRECT, ", "GEOIP,CN,DIRECT", "FINAL,DIRECT"} {
		if !strings.Contains(onlyCN, want) {
			t.Fatalf("groups=cn-ai missing %q:\n%s", want, onlyCN)
		}
	}
	if strings.Contains(onlyCN, "✈️Telegram =") || strings.Contains(onlyCN, "🔰国外流量 =") {
		t.Fatalf("groups=cn-ai should not define unselected groups:\n%s", onlyCN)
	}

	response, err = http.Get(ts.URL + "/sub?format=profile&groups=none")
	if err != nil {
		t.Fatal(err)
	}
	noGroups := readAll(t, response)
	if strings.Contains(noGroups, "🇨🇳国产AI =") || strings.Contains(noGroups, "✈️Telegram =") {
		t.Fatalf("groups=none should not define extra groups:\n%s", noGroups)
	}
	if !strings.Contains(noGroups, "GEOIP,CN,DIRECT") || !strings.Contains(noGroups, "FINAL,DIRECT") {
		t.Fatalf("groups=none should keep base rules with DIRECT fallback:\n%s", noGroups)
	}
}

func TestTokenAndValidation(t *testing.T) {
	ts, _ := newTestServer(t, "secret")

	response, err := http.Get(ts.URL + "/sub")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-token status = %d, want 401", response.StatusCode)
	}

	response, err = http.Get(ts.URL + "/sub?token=secret")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("token status = %d, want 200", response.StatusCode)
	}

	request, _ := http.NewRequest(http.MethodGet, ts.URL+"/preview", nil)
	request.Header.Set("Authorization", "Bearer secret")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("bearer status = %d, want 200", response.StatusCode)
	}

	response, err = http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200", response.StatusCode)
	}

	for _, path := range []string{"/sub?target=clash&token=secret", "/sub?format=xml&token=secret", "/sub?format=profile&groups=bogus&token=secret", "/unknown"} {
		response, err = http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		want := http.StatusBadRequest
		if strings.Contains(path, "unknown") {
			want = http.StatusNotFound
		}
		if response.StatusCode != want {
			t.Fatalf("%s status = %d, want %d", path, response.StatusCode, want)
		}
	}
}

func readAll(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestMergeQueryLabelsSources(t *testing.T) {
	ts, _ := newTestServer(t, "")
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		body := base64.StdEncoding.EncodeToString([]byte("trojan://b@b.example.com:443#Node B"))
		_, _ = io.WriteString(w, body)
	}))
	defer second.Close()

	response, err := http.Get(ts.URL + "/sub?url2=" + url.QueryEscape(second.URL) + "&label1=A&label2=B")
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.StatusCode, body)
	}
	if got := response.Header.Get("X-Sub2surge-Sources"); got != "2" {
		t.Fatalf("sources header = %q, want 2", got)
	}
	for _, want := range []string{"A - HK VMess", "B - Node B"} {
		if !strings.Contains(body, want) {
			t.Fatalf("merged body missing %q:\n%s", want, body)
		}
	}
}
