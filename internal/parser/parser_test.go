package parser

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func b64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func TestParseBase64Subscription(t *testing.T) {
	vmessPayload, err := json.Marshal(map[string]any{
		"v":    "2",
		"ps":   "HK-01",
		"add":  "hk.example.com",
		"port": "443",
		"id":   "11111111-1111-1111-1111-111111111111",
		"aid":  "0",
		"scy":  "auto",
		"net":  "ws",
		"host": "cdn.example.com",
		"path": "/ws?ed=2048",
		"tls":  "tls",
		"sni":  "cdn.example.com",
		"alpn": "h2,http/1.1",
	})
	if err != nil {
		t.Fatal(err)
	}

	lines := []string{
		"vmess://" + b64(string(vmessPayload)),
		"trojan://passw%40rd@tj.example.com:443?security=tls&sni=tj.example.com&type=ws&path=%2Fws&host=cdn.example.com&allowInsecure=1#Trojan%20US",
		"ss://" + b64("aes-256-gcm:password") + "@ss.example.com:8388#SS%20JP",
		"hysteria2://letmein@hy.example.com:443/?sni=hy.example.com&insecure=1&obfs=salamander&obfs-password=xyz#HY2",
		"tuic://uuid-1234:pass@tuic.example.com:443?alpn=h3&congestion_control=bbr&sni=tuic.example.com#TUIC",
		"vless://22222222-2222-2222-2222-222222222222@vless.example.com:443?type=ws&security=tls&sni=v.example.com&path=%2Fws#VLESS%20US",
		"ssr://" + base64.RawURLEncoding.EncodeToString([]byte("1.2.3.4:8388:auth_aes128_md5:aes-256-cfb:tls1.2_ticket_auth:cGFzcw/?remarks="+base64.RawURLEncoding.EncodeToString([]byte("SSR old")))),
	}

	result, err := Parse([]byte(b64(strings.Join(lines, "\n"))))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got, want := len(result.Proxies), 6; got != want {
		t.Fatalf("proxies = %d, want %d", got, want)
	}
	if got, want := len(result.Skipped), 1; got != want {
		t.Fatalf("skipped = %d, want %d: %+v", got, want, result.Skipped)
	}
	if result.Skipped[0].Scheme != "ssr" {
		t.Fatalf("skip scheme = %q, want ssr", result.Skipped[0].Scheme)
	}

	vmess := result.Proxies[0]
	if vmess.Type != "vmess" || vmess.Server != "hk.example.com" || vmess.Port != 443 {
		t.Fatalf("unexpected vmess: %+v", vmess)
	}
	if vmess.Network != "ws" || vmess.WSPath != "/ws" || vmess.WSHeaders["Host"] != "cdn.example.com" {
		t.Fatalf("unexpected vmess ws opts: %+v", vmess)
	}
	if !vmess.TLS || vmess.SNI != "cdn.example.com" || strings.Join(vmess.ALPN, ",") != "h2,http/1.1" {
		t.Fatalf("unexpected vmess tls: %+v", vmess)
	}
	if vmess.Cipher != "auto" || vmess.AlterID != 0 {
		t.Fatalf("unexpected vmess cipher/aid: %+v", vmess)
	}

	trojan := result.Proxies[1]
	if trojan.Password != "passw@rd" || !trojan.Insecure || trojan.WSPath != "/ws" || trojan.WSHeaders["Host"] != "cdn.example.com" {
		t.Fatalf("unexpected trojan: %+v", trojan)
	}

	ss := result.Proxies[2]
	if ss.Cipher != "aes-256-gcm" || ss.Password != "password" {
		t.Fatalf("unexpected ss: %+v", ss)
	}

	hy := result.Proxies[3]
	if hy.Password != "letmein" || hy.Obfs != "salamander" || hy.ObfsPassword != "xyz" || !hy.Insecure {
		t.Fatalf("unexpected hysteria2: %+v", hy)
	}

	tuic := result.Proxies[4]
	if tuic.UUID != "uuid-1234" || tuic.Password != "pass" || strings.Join(tuic.ALPN, ",") != "h3" || tuic.CongestionControl != "bbr" {
		t.Fatalf("unexpected tuic: %+v", tuic)
	}
}

func TestParseVMessNumericFields(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"add":  "1.2.3.4",
		"port": 8443,
		"id":   "u",
		"aid":  0,
		"tls":  true,
		"net":  "tcp",
	})
	proxy, err := ParseURI("vmess://" + b64(string(payload)))
	if err != nil {
		t.Fatalf("ParseURI: %v", err)
	}
	if proxy.Port != 8443 || !proxy.TLS || proxy.Server != "1.2.3.4" {
		t.Fatalf("unexpected proxy: %+v", proxy)
	}
}

func TestParseSSLegacyAndPlugin(t *testing.T) {
	legacy, err := ParseURI("ss://" + b64("aes-256-gcm:pass@1.2.3.4:8388#Legacy"))
	if err != nil {
		t.Fatalf("legacy: %v", err)
	}
	if legacy.Server != "1.2.3.4" || legacy.Port != 8388 || legacy.Cipher != "aes-256-gcm" || legacy.Name != "Legacy" {
		t.Fatalf("unexpected legacy ss: %+v", legacy)
	}

	plugin, err := ParseURI("ss://" + b64("aes-256-gcm:pass") + "@1.2.3.4:8388?plugin=obfs-local%3Bobfs%3Dhttp%3Bobfs-host%3Dbing.com#Obfs")
	if err != nil {
		t.Fatalf("plugin: %v", err)
	}
	if plugin.Plugin != "obfs-local" || plugin.PluginOpts["obfs"] != "http" || plugin.PluginOpts["obfs-host"] != "bing.com" {
		t.Fatalf("unexpected plugin: %+v", plugin)
	}

	// Some providers leave ";" unencoded; that must not break the parser.
	loose, err := ParseURI("ss://" + b64("aes-256-gcm:pass") + "@1.2.3.4:8388?plugin=obfs-local;obfs=http;obfs-host=bing.com#Loose")
	if err != nil {
		t.Fatalf("loose plugin: %v", err)
	}
	if loose.PluginOpts["obfs"] != "http" || loose.PluginOpts["obfs-host"] != "bing.com" {
		t.Fatalf("unexpected loose plugin: %+v", loose)
	}
}

func TestParseHysteria2PortHopping(t *testing.T) {
	proxy, err := ParseURI("hysteria2://pass@hy.example.com:443?mport=1000-2000&hop-interval=30&insecure=1#HY")
	if err != nil {
		t.Fatalf("ParseURI: %v", err)
	}
	if proxy.Ports != "1000-2000" || proxy.HopInterval != "30" || !proxy.Insecure {
		t.Fatalf("unexpected proxy: %+v", proxy)
	}
}

// Clash YAML used to be rejected outright; it is now a supported input.
func TestParseAcceptsClashConfig(t *testing.T) {
	result, err := Parse([]byte("proxies:\n  - name: a\n    type: ss\n    server: s.example.com\n    port: 443\n    cipher: aes-128-gcm\n    password: pw\nproxy-groups:\n  - name: b\n    type: select\n"))
	if err != nil {
		t.Fatalf("Clash YAML should parse now: %v", err)
	}
	if len(result.Proxies) != 1 || result.Proxies[0].Name != "a" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestParseUnknownScheme(t *testing.T) {
	proxy, err := ParseURI("snell://pass@host:443#Snell")
	if err == nil {
		t.Fatalf("expected unsupported error, got proxy %+v", proxy)
	}
	var unsupportedErr *UnsupportedError
	if !strings.Contains(err.Error(), "unsupported URI scheme") {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = unsupportedErr
}

func TestParseVLESSReality(t *testing.T) {
	proxy, err := ParseURI("vless://33333333-3333-3333-3333-333333333333@1.2.3.4:443?encryption=none&security=reality&sni=www.microsoft.com&pbk=public-key&sid=0123abcd&flow=xtls-rprx-vision#Reality")
	if err != nil {
		t.Fatalf("ParseURI: %v", err)
	}
	if !proxy.Reality || proxy.RealityPublicKey != "public-key" || proxy.RealityShortID != "0123abcd" {
		t.Fatalf("unexpected reality fields: %+v", proxy)
	}
	if proxy.Flow != "xtls-rprx-vision" || proxy.SNI != "www.microsoft.com" || proxy.Encryption != "none" {
		t.Fatalf("unexpected vless fields: %+v", proxy)
	}
}

func TestParseTUICv4Token(t *testing.T) {
	proxy, err := ParseURI("tuic://token-123@tuic.example.com:443?alpn=h3&sni=tuic.example.com#TUIC v4")
	if err != nil {
		t.Fatalf("ParseURI: %v", err)
	}
	if proxy.Token != "token-123" || proxy.UUID != "" || proxy.Password != "" {
		t.Fatalf("unexpected v4 token fields: %+v", proxy)
	}
}
