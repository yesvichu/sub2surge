// Package producer renders canonical proxies into client specific formats.
// Today only Surge is implemented, but the shape mirrors Sub-Store's
// "producers" layer so more targets can be added without touching parsers.
package producer

import (
	"fmt"
	"sort"
	"strings"

	"github.com/yesvichu/sub2surge/internal/model"
)

// UnsupportedError means the proxy is valid but Surge cannot express it.
type UnsupportedError struct {
	Reason string
}

func (e *UnsupportedError) Error() string { return e.Reason }

func unsupported(format string, args ...any) error {
	return &UnsupportedError{Reason: fmt.Sprintf(format, args...)}
}

var ssCiphers = map[string]bool{
	"2022-blake3-aes-128-gcm": true,
	"2022-blake3-aes-256-gcm": true,
	"aes-128-gcm":             true,
	"aes-192-gcm":             true,
	"aes-256-gcm":             true,
	"chacha20-ietf-poly1305":  true,
	"xchacha20-ietf-poly1305": true,
	"rc4":                     true,
	"rc4-md5":                 true,
	"aes-128-cfb":             true,
	"aes-192-cfb":             true,
	"aes-256-cfb":             true,
	"aes-128-ctr":             true,
	"aes-192-ctr":             true,
	"aes-256-ctr":             true,
	"salsa20":                 true,
	"chacha20":                true,
	"chacha20-ietf":           true,
	"none":                    true,
}

// Surge converts one canonical proxy into a single Surge policy line
// ("name = type, server, port, ...").
func Surge(proxy model.Proxy) (string, error) {
	proxy.Name = sanitizeName(proxy.Name)
	if proxy.Name == "" {
		proxy.Name = sanitizeName(fallbackName(proxy))
	}
	if proxy.Server == "" || proxy.Port < 1 || proxy.Port > 65535 {
		return "", fmt.Errorf("invalid endpoint %s:%d", proxy.Server, proxy.Port)
	}

	switch proxy.Type {
	case "ss":
		return shadowsocks(proxy)
	case "vmess":
		return vmess(proxy)
	case "vless":
		return vless(proxy)
	case "trojan":
		return trojan(proxy)
	case "hysteria2":
		return hysteria2(proxy)
	case "tuic":
		return tuic(proxy)
	case "anytls":
		return anyTLS(proxy)
	case "socks5":
		return socks5(proxy)
	case "ssr":
		return "", unsupported("Surge does not support SSR natively (SurgeMac can proxy it through an external mihomo instance)")
	default:
		return "", unsupported("Surge does not support proxy type %q", proxy.Type)
	}
}

func shadowsocks(p model.Proxy) (string, error) {
	if p.Cipher == "chacha20-poly1305" {
		p.Cipher = "chacha20-ietf-poly1305"
	}
	if p.TLS {
		return "", unsupported("Shadowsocks over TLS is not supported by Surge")
	}
	if !ssCiphers[p.Cipher] {
		return "", unsupported("Shadowsocks cipher %q is not supported by Surge", p.Cipher)
	}
	if p.Password == "" && p.Cipher != "none" {
		return "", fmt.Errorf("ss: empty password")
	}

	var b builder
	b.addf("%s=ss,%s,%d", p.Name, p.Server, p.Port)
	b.add(",encrypt-method=" + p.Cipher)
	if p.Password != "" {
		b.add(",password=" + quote(p.Password))
	}

	switch p.Plugin {
	case "":
	case "obfs", "obfs-local", "simple-obfs":
		mode := firstNonEmpty(p.PluginOpts["obfs"], p.PluginOpts["mode"], "http")
		if mode != "http" && mode != "tls" {
			return "", unsupported("Surge simple-obfs supports only http/tls, got %q", mode)
		}
		b.add(",obfs=" + mode)
		if host := firstNonEmpty(p.PluginOpts["obfs-host"], p.PluginOpts["host"]); host != "" {
			b.add(",obfs-host=" + host)
		}
		if uri := firstNonEmpty(p.PluginOpts["obfs-uri"], p.PluginOpts["path"]); uri != "" {
			b.add(",obfs-uri=" + uri)
		}
	case "shadow-tls":
		password := firstNonEmpty(p.PluginOpts["password"], p.PluginOpts["shadow-tls-password"])
		if password == "" {
			return "", fmt.Errorf("ss: shadow-tls requires a password")
		}
		b.add(",shadow-tls-password=" + quote(password))
		if host := firstNonEmpty(p.PluginOpts["host"], p.PluginOpts["sni"]); host != "" {
			b.add(",shadow-tls-sni=" + quote(host))
		}
		if version := p.PluginOpts["version"]; version != "" {
			b.add(",shadow-tls-version=" + version)
		}
	default:
		return "", unsupported("Shadowsocks plugin %q is not supported by Surge", p.Plugin)
	}

	if p.TFO {
		b.add(",tfo=true")
	}
	if p.UDP {
		b.add(",udp-relay=true")
	}
	return b.String(), nil
}

func vmess(p model.Proxy) (string, error) {
	if p.Reality {
		return "", unsupported("REALITY is not supported by Surge")
	}
	if p.UUID == "" {
		return "", fmt.Errorf("vmess: empty UUID")
	}
	if err := checkTransport(p); err != nil {
		return "", err
	}

	var b builder
	b.addf("%s=vmess,%s,%d", p.Name, p.Server, p.Port)
	b.add(",username=" + maybeQuote(p.UUID))
	if method := vmessEncryptMethod(p.Cipher); method != "" {
		b.add(",encrypt-method=" + method)
	}
	appendTransport(&b, p)
	b.addf(",vmess-aead=%t", p.AlterID == 0)
	appendTLS(&b, p, true)
	if p.TFO {
		b.add(",tfo=true")
	}
	if p.UDP {
		b.add(",udp-relay=true")
	}
	return b.String(), nil
}

func vless(p model.Proxy) (string, error) {
	if p.UUID == "" {
		return "", fmt.Errorf("vless: empty UUID")
	}
	if p.Encryption != "" && !strings.EqualFold(p.Encryption, "none") {
		return "", unsupported("VLESS encryption %q is not supported by Surge", p.Encryption)
	}

	switch p.Network {
	case "", "tcp":
	case "ws":
		if p.Reality {
			return "", unsupported("Surge does not support VLESS REALITY over WebSocket")
		}
	case "grpc":
		if !p.TLS && !p.Reality {
			return "", unsupported("Surge VLESS gRPC requires TLS or REALITY")
		}
	case "h2", "http", "httpupgrade", "kcp", "quic":
		return "", unsupported("Surge does not support VLESS %s transport", p.Network)
	default:
		return "", unsupported("Surge does not support VLESS transport %q", p.Network)
	}

	if p.Reality {
		if p.RealityPublicKey == "" {
			return "", fmt.Errorf("vless: REALITY requires pbk (reality-public-key)")
		}
		if p.SNI == "" {
			return "", fmt.Errorf("vless: REALITY requires sni")
		}
	}

	var b builder
	b.addf("%s=vless,%s,%d", p.Name, p.Server, p.Port)
	b.add(",username=" + maybeQuote(p.UUID))

	// Vision is only valid on RAW/TCP with TLS or REALITY, and Surge only
	// accepts the two Xray vision flow names.
	if flow := p.Flow; (flow == "xtls-rprx-vision" || flow == "xtls-rprx-vision-udp443") &&
		(p.Network == "" || p.Network == "tcp") && (p.TLS || p.Reality) {
		b.add(",flow=" + flow)
	}

	switch p.Network {
	case "ws":
		appendWS(&b, p)
	case "grpc":
		b.add(",grpc=true")
		if p.GRPCServiceName != "" {
			b.add(",grpc-service-name=" + maybeQuote(p.GRPCServiceName))
		}
	}

	if p.Reality {
		b.add(",reality-public-key=" + p.RealityPublicKey)
		if p.RealityShortID != "" {
			b.add(",reality-short-id=" + p.RealityShortID)
		}
	} else if p.TLS {
		b.add(",tls=true")
	}

	if p.TLS || p.Reality {
		appendTLSParams(&b, p, !p.Reality)
	}
	if p.TFO {
		b.add(",tfo=true")
	}
	if p.UDP {
		b.add(",udp-relay=true")
	}
	return b.String(), nil
}

func trojan(p model.Proxy) (string, error) {
	if p.Password == "" {
		return "", fmt.Errorf("trojan: empty password")
	}
	if err := checkTransport(p); err != nil {
		return "", err
	}

	var b builder
	b.addf("%s=trojan,%s,%d", p.Name, p.Server, p.Port)
	b.add(",password=" + quote(p.Password))
	appendTransport(&b, p)
	appendTLSParams(&b, p, true)
	if p.TFO {
		b.add(",tfo=true")
	}
	if p.UDP {
		b.add(",udp-relay=true")
	}
	return b.String(), nil
}

func hysteria2(p model.Proxy) (string, error) {
	if p.Password == "" {
		return "", fmt.Errorf("hysteria2: empty password")
	}
	if p.Obfs != "" {
		switch strings.ToLower(p.Obfs) {
		case "salamander", "gecko":
		default:
			return "", unsupported("Hysteria2 obfs %q is not supported by Surge (only salamander/gecko)", p.Obfs)
		}
	}

	var b builder
	b.addf("%s=hysteria2,%s,%d", p.Name, p.Server, p.Port)
	b.add(",password=" + quote(p.Password))
	if p.Ports != "" {
		b.add(",port-hopping=" + quote(strings.ReplaceAll(p.Ports, ",", ";")))
	}
	if p.HopInterval != "" {
		b.add(",port-hopping-interval=" + p.HopInterval)
	}
	if p.DownloadMbps != "" {
		b.add(",download-bandwidth=" + p.DownloadMbps)
	}
	if p.Obfs != "" {
		if p.ObfsPassword == "" {
			return "", fmt.Errorf("hysteria2: obfs %s requires a password", p.Obfs)
		}
		field := "salamander-password"
		if strings.EqualFold(p.Obfs, "gecko") {
			field = "gecko-password"
		}
		b.add("," + field + "=" + quote(p.ObfsPassword))
	}
	appendTLS(&b, p, false)
	if p.TFO {
		b.add(",tfo=true")
	}
	return b.String(), nil
}

func tuic(p model.Proxy) (string, error) {
	kind := "tuic-v5"
	if p.Token != "" {
		kind = "tuic"
	}

	var b builder
	b.addf("%s=%s,%s,%d", p.Name, kind, p.Server, p.Port)
	if p.Token != "" {
		b.add(",token=" + maybeQuote(p.Token))
	} else {
		if p.UUID == "" || p.Password == "" {
			return "", fmt.Errorf("tuic-v5: uuid and password are required")
		}
		b.add(",uuid=" + maybeQuote(p.UUID))
		b.add(",password=" + quote(p.Password))
	}
	if p.Ports != "" {
		b.add(",port-hopping=" + quote(strings.ReplaceAll(p.Ports, ",", ";")))
	}
	if p.HopInterval != "" {
		b.add(",port-hopping-interval=" + p.HopInterval)
	}
	appendTLS(&b, p, false)
	if p.TFO {
		b.add(",tfo=true")
	}
	return b.String(), nil
}

func anyTLS(p model.Proxy) (string, error) {
	if p.Password == "" {
		return "", fmt.Errorf("anytls: empty password")
	}

	var b builder
	b.addf("%s=anytls,%s,%d", p.Name, p.Server, p.Port)
	b.add(",password=" + quote(p.Password))
	appendTLS(&b, p, false)
	if p.TFO {
		b.add(",tfo=true")
	}
	return b.String(), nil
}

func socks5(p model.Proxy) (string, error) {
	kind := "socks5"
	if p.TLS {
		kind = "socks5-tls"
	}

	var b builder
	b.addf("%s=%s,%s,%d", p.Name, kind, p.Server, p.Port)
	if p.Username != "" {
		b.add(",username=" + quote(p.Username))
	}
	if p.Password != "" {
		b.add(",password=" + quote(p.Password))
	}
	if p.TLS {
		appendTLSParams(&b, p, true)
	}
	if p.UDP {
		b.add(",udp-relay=true")
	}
	return b.String(), nil
}

func checkTransport(p model.Proxy) error {
	if p.Reality {
		return unsupported("REALITY is not supported by Surge")
	}
	switch p.Network {
	case "", "tcp":
		return nil
	case "ws":
		return nil
	case "grpc":
		return unsupported("Surge does not support gRPC transport")
	case "h2", "http":
		return unsupported("Surge does not support HTTP/2 or HTTP-header transport")
	case "httpupgrade":
		return unsupported("Surge does not support httpupgrade transport")
	case "quic", "kcp":
		return unsupported("Surge does not support %s transport", p.Network)
	default:
		return unsupported("Surge does not support transport %q", p.Network)
	}
}

func appendTransport(b *builder, p model.Proxy) {
	if p.Network == "ws" {
		appendWS(b, p)
	}
}

func appendWS(b *builder, p model.Proxy) {
	b.add(",ws=true")
	path := p.WSPath
	if path == "" {
		path = "/"
	}
	b.add(",ws-path=" + maybeQuote(path))
	if len(p.WSHeaders) > 0 {
		b.add(",ws-headers=" + quote(formatHeaders(p.WSHeaders)))
	}
}

func appendTLS(b *builder, p model.Proxy, includeTLSFlag bool) {
	if includeTLSFlag {
		if !p.TLS {
			return
		}
		b.add(",tls=true")
	}
	appendTLSParams(b, p, true)
}

func appendTLSParams(b *builder, p model.Proxy, allowInsecure bool) {
	if p.SNI != "" {
		b.add(",sni=" + quote(p.SNI))
	}
	if len(p.ALPN) > 0 {
		b.add(",alpn=" + quote(strings.Join(p.ALPN, ",")))
	}
	if allowInsecure && p.Insecure {
		b.add(",skip-cert-verify=true")
	}
	if p.Fingerprint != "" {
		b.add(",server-cert-fingerprint-sha256=" + p.Fingerprint)
	}
}

func vmessEncryptMethod(cipher string) string {
	switch strings.ToLower(strings.TrimSpace(cipher)) {
	case "aes-128-gcm":
		return "aes-128-gcm"
	case "chacha20-poly1305", "chacha20-ietf-poly1305":
		return "chacha20-ietf-poly1305"
	default:
		// auto, none, zero and unknown values all fall back to Surge's default.
		return ""
	}
}

func formatHeaders(headers map[string]string) string {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.ReplaceAll(headers[key], "|", ";")
		parts = append(parts, key+":"+value)
	}
	return strings.Join(parts, "|")
}

func sanitizeName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case '=', ',', '"', '\n', '\r', '\t':
			return -1
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	// A policy line starting with #/;// would be parsed as a comment, and "["
	// could be mistaken for a section header.
	if strings.HasPrefix(name, "#") || strings.HasPrefix(name, "//") ||
		strings.HasPrefix(name, ";") || strings.HasPrefix(name, "[") {
		name = "_" + name
	}
	return name
}

func fallbackName(p model.Proxy) string {
	switch p.Type {
	case "hysteria2":
		return fmt.Sprintf("Hysteria2 %s:%d", p.Server, p.Port)
	case "tuic":
		return fmt.Sprintf("TUIC %s:%d", p.Server, p.Port)
	case "anytls":
		return fmt.Sprintf("AnyTLS %s:%d", p.Server, p.Port)
	case "vmess":
		return fmt.Sprintf("VMess %s:%d", p.Server, p.Port)
	case "vless":
		return fmt.Sprintf("VLESS %s:%d", p.Server, p.Port)
	case "trojan":
		return fmt.Sprintf("Trojan %s:%d", p.Server, p.Port)
	case "socks5":
		return fmt.Sprintf("SOCKS5 %s:%d", p.Server, p.Port)
	default:
		return fmt.Sprintf("%s %s:%d", p.Type, p.Server, p.Port)
	}
}

func quote(value string) string {
	value = strings.Trim(value, `"`)
	value = strings.ReplaceAll(value, `"`, `'`)
	return `"` + value + `"`
}

func maybeQuote(value string) string {
	if strings.ContainsAny(value, ",\" \t") {
		return quote(value)
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type builder struct {
	b strings.Builder
}

func (b *builder) add(value string) {
	b.b.WriteString(value)
}

func (b *builder) addf(format string, args ...any) {
	fmt.Fprintf(&b.b, format, args...)
}

func (b *builder) String() string {
	return b.b.String()
}
