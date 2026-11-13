// Package model contains the platform-neutral proxy representation used by
// sub2surge. It intentionally mirrors the shape of Sub-Store's internal
// (mihomo-like) proxy objects: parsers only fill fields, producers only read
// them, and neither side knows about the other.
package model

// Proxy is a single node in the canonical, client-neutral form.
type Proxy struct {
	Name   string
	Type   string // ss, vmess, vless, trojan, hysteria2, tuic, anytls, socks5, ssr...
	Server string
	Port   int

	// Authentication / ciphers.
	UUID     string
	Username string
	Password string
	Cipher   string
	AlterID  int

	// TLS & transport.
	Network     string // "", tcp, ws, grpc, h2, http, httpupgrade
	TLS         bool
	Reality     bool
	SNI         string
	ALPN        []string
	Insecure    bool
	Fingerprint string
	Flow        string

	// VLESS / REALITY.
	RealityPublicKey string
	RealityShortID   string
	Encryption       string

	WSPath          string
	WSHeaders       map[string]string
	GRPCServiceName string

	// Shadowsocks plugins.
	Plugin     string // obfs (simple-obfs / obfs-local)
	PluginOpts map[string]string

	// Hysteria2 / TUIC.
	Obfs         string
	ObfsPassword string
	Ports        string
	HopInterval  string

	Token             string
	CongestionControl string
	UDPRelayMode      string
	DownloadMbps      string

	// Per-node switches.
	UDP bool
	TFO bool
}

// Skip records a URI that could not be converted, with a human readable reason.
type Skip struct {
	Line   int    `json:"line,omitempty"`
	Scheme string `json:"scheme,omitempty"`
	Name   string `json:"name,omitempty"`
	Reason string `json:"reason"`
}

// Result is the output of the parse stage.
type Result struct {
	Proxies  []Proxy
	Skipped  []Skip
	Warnings []string
}
