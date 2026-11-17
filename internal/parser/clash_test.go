package parser

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/yesvichu/sub2surge/internal/model"
)

func byeKey(proxies []model.Proxy, name string) (model.Proxy, bool) {
	for _, proxy := range proxies {
		if proxy.Name == name {
			return proxy, true
		}
	}
	return model.Proxy{}, false
}

func skipByName(skips []model.Skip, name string) (model.Skip, bool) {
	for _, skip := range skips {
		if skip.Name == name {
			return skip, true
		}
	}
	return model.Skip{}, false
}

const clashFixture = `# mihomo profile
port: 7890
mode: rule

proxies:
  - name: SS-1
    type: ss
    server: ss.example.com
    port: 8388
    cipher: aes-256-gcm
    password: "ss pass"
    udp: true

  - name: SS-OBFS
    type: ss
    server: obfs.example.com
    port: 443
    cipher: chacha20-ietf-poly1305
    password: pw
    plugin: obfs
    plugin-opts:
      mode: tls
      host: bing.com

  - name: VM-WS
    type: vmess
    server: vm.example.com
    port: 443
    uuid: 11111111-2222-3333-4444-555555555555
    alterId: 0
    cipher: auto
    tls: true
    servername: cdn.example.com
    network: ws
    skip-cert-verify: true
    alpn: [h2, http/1.1]
    ws-opts:
      path: /ws
      headers:
        Host: cdn.example.com
    udp: true

  - name: VL-REALITY
    type: vless
    server: vl.example.com
    port: 443
    uuid: 11111111-2222-3333-4444-666666666666
    tls: true
    servername: www.microsoft.com
    flow: xtls-rprx-vision
    client-fingerprint: chrome
    reality-opts:
      public-key: pubkey123
      short-id: abcd

  - name: TR-1
    type: trojan
    server: tr.example.com
    port: 443
    password: trpw
    sni: tr.example.com
    skip-cert-verify: true

  - name: HY2-1
    type: hysteria2
    server: hy.example.com
    port: 443
    password: hypw
    obfs: salamander
    obfs-password: obfspw
    sni: hy.example.com
    up: 30
    down: 200

  - name: TUIC-1
    type: tuic
    server: tuic.example.com
    port: 443
    uuid: 11111111-2222-3333-4444-777777777777
    password: tuicpw
    alpn: [h3]
    sni: tuic.example.com

  - name: ANY-1
    type: anytls
    server: any.example.com
    port: 443
    password: anypw
    sni: any.example.com

  - name: SOCKS-1
    type: socks5
    server: socks.example.com
    port: 1080
    username: user
    password: pass

  - name: SSR-1
    type: ssr
    server: ssr.example.com
    port: 443
    cipher: aes-256-cfb
    password: pw

  - name: SNELL-1
    type: snell
    server: snell.example.com
    port: 443
    psk: psk

  - name: DIRECT-1
    type: direct

  - name: BROKEN
    type: ss
    server: broken.example.com
    port: 99999
    cipher: aes-128-gcm
    password: pw

proxy-groups:
  - name: PROXY
    type: select
    proxies: [SS-1, VM-WS]

rules:
  - MATCH,PROXY
`

func TestParseClashMapsSupportedTypes(t *testing.T) {
	result, err := ParseClash([]byte(clashFixture))
	if err != nil {
		t.Fatalf("ParseClash: %v", err)
	}

	if len(result.Proxies) != 9 {
		t.Fatalf("proxies = %d, want 9: %+v", len(result.Proxies), result.Proxies)
	}

	ss, ok := byeKey(result.Proxies, "SS-1")
	if !ok {
		t.Fatal("SS-1 missing")
	}
	if ss.Type != "ss" || ss.Cipher != "aes-256-gcm" || ss.Password != "ss pass" || !ss.UDP {
		t.Errorf("SS-1 = %+v", ss)
	}

	obfs, _ := byeKey(result.Proxies, "SS-OBFS")
	if obfs.Plugin != "obfs" || obfs.PluginOpts["obfs"] != "tls" || obfs.PluginOpts["host"] != "bing.com" {
		t.Errorf("SS-OBFS plugin = %+v %+v", obfs.Plugin, obfs.PluginOpts)
	}

	vm, _ := byeKey(result.Proxies, "VM-WS")
	if vm.Type != "vmess" || vm.UUID == "" || vm.Network != "ws" || !vm.TLS {
		t.Errorf("VM-WS = %+v", vm)
	}
	if vm.SNI != "cdn.example.com" || vm.WSPath != "/ws" || vm.WSHeaders["Host"] != "cdn.example.com" {
		t.Errorf("VM-WS transport = %+v", vm)
	}
	if !vm.Insecure || len(vm.ALPN) != 2 || !vm.UDP {
		t.Errorf("VM-WS flags = %+v", vm)
	}

	vl, _ := byeKey(result.Proxies, "VL-REALITY")
	if !vl.Reality || vl.RealityPublicKey != "pubkey123" || vl.RealityShortID != "abcd" {
		t.Errorf("VL-REALITY = %+v", vl)
	}
	if vl.Flow != "xtls-rprx-vision" || vl.Fingerprint != "chrome" {
		t.Errorf("VL-REALITY flow/fingerprint = %+v", vl)
	}

	tr, _ := byeKey(result.Proxies, "TR-1")
	if tr.Type != "trojan" || tr.Password != "trpw" || !tr.Insecure {
		t.Errorf("TR-1 = %+v", tr)
	}

	hy, _ := byeKey(result.Proxies, "HY2-1")
	if hy.Type != "hysteria2" || hy.Obfs != "salamander" || hy.ObfsPassword != "obfspw" {
		t.Errorf("HY2-1 = %+v", hy)
	}
	if hy.DownloadMbps != "200" {
		t.Errorf("HY2-1 download = %q, want 200", hy.DownloadMbps)
	}

	tuic, _ := byeKey(result.Proxies, "TUIC-1")
	if tuic.Type != "tuic" || tuic.UUID == "" || tuic.Password != "tuicpw" || len(tuic.ALPN) != 1 {
		t.Errorf("TUIC-1 = %+v", tuic)
	}

	anytls, _ := byeKey(result.Proxies, "ANY-1")
	if anytls.Type != "anytls" || anytls.Password != "anypw" {
		t.Errorf("ANY-1 = %+v", anytls)
	}

	socks, _ := byeKey(result.Proxies, "SOCKS-1")
	if socks.Type != "socks5" || socks.Username != "user" || socks.Password != "pass" {
		t.Errorf("SOCKS-1 = %+v", socks)
	}

	if len(result.Skipped) != 4 {
		t.Fatalf("skips = %d, want 4: %+v", len(result.Skipped), result.Skipped)
	}
	for _, name := range []string{"SSR-1", "SNELL-1", "DIRECT-1", "BROKEN"} {
		if _, ok := skipByName(result.Skipped, name); !ok {
			t.Errorf("%s was not reported as skipped: %+v", name, result.Skipped)
		}
	}
}

func TestParseClashRejectsUnreadableBodies(t *testing.T) {
	for _, body := range []string{
		"proxies: []\n",
		"proxies: [\n",
		`{"outbounds": []}`,
		"port: 7890\nrules: []\n",
	} {
		if _, err := ParseClash([]byte(body)); err == nil {
			t.Errorf("ParseClash(%q) should fail", body)
		}
	}
}

func TestParseClashSkipsDuplicateNodes(t *testing.T) {
	body := `proxies:
  - {name: A, type: ss, server: x.example.com, port: 443, cipher: aes-128-gcm, password: pw}
  - {name: B, type: ss, server: x.example.com, port: 443, cipher: aes-128-gcm, password: pw}
`
	result, err := ParseClash([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Proxies) != 1 {
		t.Errorf("same endpoint kept twice: %+v", result.Proxies)
	}
}

const shadowrocketFixture = `#!MANAGED-CONFIG https://example.com/sub

[General]
loglevel = notify

[Proxy]
HK-SS = ss, hk.example.com, 443, encrypt-method=aes-256-gcm, password="p,w", udp-relay=true
US-VM = vmess, us.example.com, 443, username=11111111-2222-3333-4444-555555555555, ws=true, ws-path=/ws, ws-headers=Host:cdn.example.com, tls=true, sni=cdn.example.com, skip-cert-verify=true, vmess-aead=true
JP-TR = trojan, jp.example.com, 443, password=trpw, sni=jp.example.com
HY-SG = hysteria2, sg.example.com, 443, password=hypw, port-hopping="443;8443", download-bandwidth=200, salamander-password=obfspw
TU-DE = tuic-v5, de.example.com, 443, uuid=11111111-2222-3333-4444-666666666666, password=tuicpw, alpn=h3
ANY-FR = anytls, fr.example.com, 443, password=anypw, sni=fr.example.com
SOCKS-UK = socks5-tls, uk.example.com, 1080, username=user, password=pass, skip-cert-verify=true
SNELL-1 = snell, sn.example.com, 443, psk=psk

[Proxy Group]
PROXY = select, HK-SS, US-VM

[Rule]
FINAL,PROXY
`

func TestParseSurgeConfigReadsProxySection(t *testing.T) {
	result, err := ParseSurgeConfig([]byte(shadowrocketFixture))
	if err != nil {
		t.Fatalf("ParseSurgeConfig: %v", err)
	}

	if len(result.Proxies) != 7 {
		t.Fatalf("proxies = %d, want 7: %+v", len(result.Proxies), result.Proxies)
	}

	ss, ok := byeKey(result.Proxies, "HK-SS")
	if !ok {
		t.Fatal("HK-SS missing")
	}
	if ss.Type != "ss" || ss.Cipher != "aes-256-gcm" || ss.Password != "p,w" || !ss.UDP {
		t.Errorf("HK-SS = %+v", ss)
	}

	vm, _ := byeKey(result.Proxies, "US-VM")
	if vm.UUID == "" || vm.Network != "ws" || vm.WSPath != "/ws" {
		t.Errorf("US-VM = %+v", vm)
	}
	if vm.WSHeaders["Host"] != "cdn.example.com" || !vm.TLS || !vm.Insecure {
		t.Errorf("US-VM headers/tls = %+v", vm)
	}
	if vm.AlterID != 0 {
		t.Errorf("vmess-aead=true should keep AlterID 0, got %d", vm.AlterID)
	}

	tr, _ := byeKey(result.Proxies, "JP-TR")
	if tr.Type != "trojan" || tr.Password != "trpw" || tr.SNI != "jp.example.com" {
		t.Errorf("JP-TR = %+v", tr)
	}

	hy, _ := byeKey(result.Proxies, "HY-SG")
	if hy.Type != "hysteria2" || hy.Ports != "443;8443" || hy.DownloadMbps != "200" {
		t.Errorf("HY-SG = %+v", hy)
	}
	if hy.Obfs != "salamander" || hy.ObfsPassword != "obfspw" {
		t.Errorf("HY-SG obfs = %+v", hy)
	}

	tuic, _ := byeKey(result.Proxies, "TU-DE")
	if tuic.Type != "tuic" || tuic.UUID == "" || len(tuic.ALPN) != 1 {
		t.Errorf("TU-DE = %+v", tuic)
	}

	socks, _ := byeKey(result.Proxies, "SOCKS-UK")
	if socks.Type != "socks5" || !socks.TLS || socks.Username != "user" {
		t.Errorf("SOCKS-UK = %+v", socks)
	}

	if _, ok := skipByName(result.Skipped, "SNELL-1"); !ok {
		t.Errorf("SNELL-1 was not reported as skipped: %+v", result.Skipped)
	}
}

func TestParseSurgeConfigRejectsBodiesWithoutProxySection(t *testing.T) {
	for _, body := range []string{
		"[Rule]\nFINAL,DIRECT\n",
		"[Proxy]\n",
	} {
		if _, err := ParseSurgeConfig([]byte(body)); err == nil {
			t.Errorf("ParseSurgeConfig(%q) should fail", body)
		}
	}

	// A [Proxy] section whose lines are all unusable reports them as skips,
	// the same way an all-unsupported URI list does, so the page can show why.
	result, err := ParseSurgeConfig([]byte("[Proxy]\nnot a proxy line\n"))
	if err != nil {
		t.Fatalf("unusable lines should be reported, not fail: %v", err)
	}
	if len(result.Proxies) != 0 || len(result.Skipped) != 1 {
		t.Errorf("result = %+v", result)
	}
}

// TestParseRoutesToStructuredReaders covers the detection in Parse, including
// the base64-wrapped Clash body providers sometimes serve.
func TestParseRoutesToStructuredReaders(t *testing.T) {
	clash, err := Parse([]byte(clashFixture))
	if err != nil {
		t.Fatalf("Clash via Parse: %v", err)
	}
	if len(clash.Proxies) != 9 {
		t.Errorf("Clash via Parse gave %d proxies", len(clash.Proxies))
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(clashFixture))
	decoded, err := Parse([]byte(encoded))
	if err != nil {
		t.Fatalf("base64 Clash via Parse: %v", err)
	}
	if len(decoded.Proxies) != 9 {
		t.Errorf("base64 Clash via Parse gave %d proxies", len(decoded.Proxies))
	}

	surge, err := Parse([]byte(shadowrocketFixture))
	if err != nil {
		t.Fatalf("Shadowrocket via Parse: %v", err)
	}
	if len(surge.Proxies) != 7 {
		t.Errorf("Shadowrocket via Parse gave %d proxies", len(surge.Proxies))
	}

	// A URI list still goes down the original path.
	uris := "trojan://pw@a.example.com:443#A\n"
	fromURIs, err := Parse([]byte(uris))
	if err != nil || len(fromURIs.Proxies) != 1 {
		t.Errorf("URI list via Parse = %+v, %v", fromURIs, err)
	}

	// sing-box JSON stays unsupported, and says so.
	if _, err := Parse([]byte(`{"outbounds":[{"type":"vmess"}]}`)); err == nil ||
		!strings.Contains(err.Error(), "sing-box") {
		t.Errorf("sing-box JSON error = %v", err)
	}
}

// YAML resolves "0123" to the integer 83 (octal) when scalars are decoded into
// any, which would silently corrupt REALITY short IDs. Scalars are read as
// written instead.
func TestParseClashKeepsLeadingZeros(t *testing.T) {
	body := `proxies:
  - name: 0123
    type: vless
    server: r.example.com
    port: 443
    uuid: 11111111-2222-3333-4444-555555555555
    tls: true
    servername: www.microsoft.com
    reality-opts:
      public-key: PBK
      short-id: 0123
    skip-cert-verify: no
  - name: PASSWORD
    type: ss
    server: p.example.com
    port: 8388
    cipher: aes-128-gcm
    password: 0012
`
	result, err := ParseClash([]byte(body))
	if err != nil {
		t.Fatalf("ParseClash: %v", err)
	}

	reality, ok := byeKey(result.Proxies, "0123")
	if !ok {
		t.Fatalf("node named 0123 missing: %+v", result.Proxies)
	}
	if reality.RealityShortID != "0123" {
		t.Errorf("short-id = %q, want %q", reality.RealityShortID, "0123")
	}
	if reality.Insecure {
		t.Error(`skip-cert-verify: no should stay false`)
	}

	ss, _ := byeKey(result.Proxies, "PASSWORD")
	if ss.Password != "0012" {
		t.Errorf("password = %q, want %q", ss.Password, "0012")
	}
}

// Anchors are common in hand-edited profiles; ws-opts may be shared.
func TestParseClashFollowsAnchors(t *testing.T) {
	body := `proxies:
  - name: A
    type: vmess
    server: a.example.com
    port: 443
    uuid: u-u-i-d
    network: ws
    ws-opts: &wso {path: /shared}
  - name: B
    type: vmess
    server: b.example.com
    port: 443
    uuid: u-u-i-d
    network: ws
    ws-opts: *wso
`
	result, err := ParseClash([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"A", "B"} {
		proxy, ok := byeKey(result.Proxies, name)
		if !ok || proxy.WSPath != "/shared" {
			t.Errorf("%s ws path = %q", name, proxy.WSPath)
		}
	}
}
