package parser

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/yesvichu/sub2surge/internal/model"
)

// ParseURI parses one proxy URI into the canonical model. It does not reject
// protocols that Surge lacks: that decision belongs to the producer, so the
// preview endpoint can still show what was found.
func ParseURI(line string) (model.Proxy, error) {
	line = strings.TrimSpace(line)
	switch schemeOf(line) {
	case "vmess":
		return parseVMess(line)
	case "vless":
		return parseVLESS(line)
	case "trojan":
		return parseTrojan(line)
	case "ss":
		return parseSS(line)
	case "ssr":
		return parseSSR(line)
	case "hysteria2", "hy2":
		return parseHysteria2(line)
	case "hysteria":
		return model.Proxy{}, unsupported("hysteria", fragmentName(line),
			"Surge only supports Hysteria2; Hysteria v1 is not supported")
	case "tuic":
		return parseTUIC(line)
	case "anytls":
		return parseAnyTLS(line)
	case "socks5", "socks5h", "socks", "socks5+tls":
		return parseSocks5(line)
	case "wireguard", "wg":
		return model.Proxy{}, unsupported(schemeOf(line), fragmentName(line),
			"WireGuard URIs are not supported by this tool; Surge needs a WireGuard profile/module instead")
	case "http", "https":
		return model.Proxy{}, unsupported(schemeOf(line), fragmentName(line),
			"HTTP(S) URIs are web links, not proxy nodes; Surge HTTP proxies need host/port/credentials, which this URI form does not carry")
	case "":
		return model.Proxy{}, fmt.Errorf("not a URI")
	default:
		return model.Proxy{}, unsupported(schemeOf(line), fragmentName(line),
			fmt.Sprintf("unsupported URI scheme %q", schemeOf(line)))
	}
}

func parseVLESS(line string) (model.Proxy, error) {
	proxy, err := parseURLNode(line, "vless")
	if err != nil {
		return model.Proxy{}, err
	}
	if strings.TrimSpace(proxy.UUID) == "" {
		return model.Proxy{}, fmt.Errorf("vless: missing UUID")
	}
	return proxy, nil
}

func parseTrojan(line string) (model.Proxy, error) {
	proxy, err := parseURLNode(line, "trojan")
	if err != nil {
		return model.Proxy{}, err
	}
	if strings.TrimSpace(proxy.Password) == "" {
		return model.Proxy{}, fmt.Errorf("trojan: missing password")
	}
	return proxy, nil
}

func parseHysteria2(line string) (model.Proxy, error) {
	proxy, err := parseURLNode(line, "hysteria2")
	if err != nil {
		return model.Proxy{}, err
	}
	if strings.TrimSpace(proxy.Password) == "" {
		return model.Proxy{}, fmt.Errorf("hysteria2: missing password")
	}
	return proxy, nil
}

func parseTUIC(line string) (model.Proxy, error) {
	proxy, err := parseURLNode(line, "tuic")
	if err != nil {
		return model.Proxy{}, err
	}
	if strings.TrimSpace(proxy.UUID) == "" && strings.TrimSpace(proxy.Token) == "" {
		return model.Proxy{}, fmt.Errorf("tuic: missing UUID/token")
	}
	if strings.TrimSpace(proxy.Password) == "" && strings.TrimSpace(proxy.Token) == "" {
		return model.Proxy{}, fmt.Errorf("tuic: missing password/token")
	}
	return proxy, nil
}

func parseAnyTLS(line string) (model.Proxy, error) {
	proxy, err := parseURLNode(line, "anytls")
	if err != nil {
		return model.Proxy{}, err
	}
	if strings.TrimSpace(proxy.Password) == "" {
		return model.Proxy{}, fmt.Errorf("anytls: missing password")
	}
	return proxy, nil
}

func parseSocks5(line string) (model.Proxy, error) {
	proxy, err := parseURLNode(line, "socks5")
	if err != nil {
		return model.Proxy{}, err
	}
	proxy.TLS = strings.HasSuffix(strings.ToLower(schemeOf(line)), "+tls")
	if !proxy.TLS {
		if u, err := url.Parse(line); err == nil {
			proxy.TLS = queryBool(u.Query(), "tls", "security")
		}
	}
	return proxy, nil
}

func parseURLNode(line, kind string) (model.Proxy, error) {
	u, err := url.Parse(line)
	if err != nil {
		// Some providers leave a literal space in the fragment.
		u, err = url.Parse(strings.ReplaceAll(line, " ", "%20"))
		if err != nil {
			return model.Proxy{}, fmt.Errorf("%s: invalid URI: %w", kind, err)
		}
	}

	server := u.Hostname()
	if server == "" {
		return model.Proxy{}, fmt.Errorf("%s: missing server", kind)
	}
	port, err := urlPort(u, defaultPort(kind))
	if err != nil {
		return model.Proxy{}, fmt.Errorf("%s: %w", kind, err)
	}

	name := strings.TrimSpace(u.Fragment)
	if name == "" {
		name = defaultName(kind, server, port)
	}

	proxy := model.Proxy{
		Name:   name,
		Type:   kind,
		Server: server,
		Port:   port,
	}
	q := u.Query()

	switch kind {
	case "vless", "vmess":
		if u.User != nil {
			proxy.UUID = u.User.Username()
		}
	case "trojan", "hysteria2", "anytls":
		proxy.Password = fullUserinfo(u.User)
	case "tuic":
		if u.User != nil {
			proxy.UUID = u.User.Username()
			proxy.Password, _ = u.User.Password()
		}
	case "socks5":
		if u.User != nil {
			proxy.Username = u.User.Username()
			proxy.Password, _ = u.User.Password()
		}
	}

	security := strings.ToLower(q.Get("security"))
	switch kind {
	case "trojan", "hysteria2", "tuic", "anytls":
		proxy.TLS = true
	case "vless", "vmess":
		proxy.TLS = security == "tls" || security == "xtls" || security == "reality"
	}
	proxy.Reality = security == "reality"
	proxy.Flow = q.Get("flow")
	if kind == "vless" {
		proxy.RealityPublicKey = firstNonEmpty(
			q.Get("pbk"), q.Get("publicKey"), q.Get("public-key"),
		)
		proxy.RealityShortID = firstNonEmpty(
			q.Get("sid"), q.Get("shortId"), q.Get("short-id"),
		)
		proxy.Encryption = q.Get("encryption")
		if proxy.RealityPublicKey != "" {
			proxy.Reality = true
		}
	}

	hostHeader := wsHost(q.Get("host"))
	proxy.SNI = firstNonEmpty(
		q.Get("sni"),
		q.Get("peer"),
		q.Get("servername"),
	)
	if proxy.TLS && proxy.SNI == "" {
		proxy.SNI = hostHeader
	}
	proxy.ALPN = queryList(q, "alpn")
	proxy.Insecure = queryBool(q,
		"allowInsecure", "allow_insecure", "insecure",
		"skip-cert-verify", "skip_cert_verify",
	)

	if kind != "hysteria2" && kind != "tuic" && kind != "anytls" && kind != "socks5" {
		network := strings.ToLower(q.Get("type"))
		headerType := strings.ToLower(q.Get("headerType"))
		switch network {
		case "", "tcp", "raw", "none":
			if headerType == "http" {
				proxy.Network = "http"
			} else {
				proxy.Network = "tcp"
			}
		case "ws", "websocket":
			proxy.Network = "ws"
			proxy.WSPath = cleanWSPath(q.Get("path"))
			if hostHeader != "" {
				proxy.WSHeaders = map[string]string{"Host": hostHeader}
			}
		case "grpc":
			proxy.Network = "grpc"
			proxy.GRPCServiceName = firstNonEmpty(q.Get("serviceName"), q.Get("path"))
		case "http", "h2", "h2c":
			proxy.Network = "h2"
		case "httpupgrade":
			proxy.Network = "httpupgrade"
		case "quic", "kcp":
			proxy.Network = network
		default:
			proxy.Network = network
		}
	}

	switch kind {
	case "vmess":
		proxy.Cipher = firstNonEmpty(q.Get("scy"), "auto")
		proxy.AlterID = anyInt(q.Get("aid"))
	case "hysteria2":
		if obfs := q.Get("obfs"); obfs != "" && !strings.EqualFold(obfs, "none") {
			proxy.Obfs = obfs
		}
		proxy.ObfsPassword = q.Get("obfs-password")
		proxy.Ports = firstNonEmpty(q.Get("mport"), q.Get("ports"))
		proxy.HopInterval = firstNonEmpty(q.Get("hop-interval"), q.Get("hop_interval"))
		proxy.Fingerprint = q.Get("pinSHA256")
		proxy.DownloadMbps = firstNonEmpty(q.Get("downmbps"), q.Get("down"))
		if proxy.Password == "" {
			proxy.Password = firstNonEmpty(q.Get("auth"), q.Get("password"))
		}
	case "tuic":
		if token := q.Get("token"); token != "" {
			proxy.Token = token
		}
		proxy.CongestionControl = firstNonEmpty(
			q.Get("congestion_control"), q.Get("congestion-control"),
		)
		proxy.UDPRelayMode = firstNonEmpty(
			q.Get("udp_relay_mode"), q.Get("udp-relay-mode"),
		)
		if proxy.Password == "" {
			proxy.Password = q.Get("password")
		}
		// TUIC v4 share links carry a single token in the userinfo slot.
		if proxy.Password == "" && proxy.Token == "" && proxy.UUID != "" {
			proxy.Token = proxy.UUID
			proxy.UUID = ""
		}
	case "anytls":
		if proxy.Password == "" {
			proxy.Password = q.Get("password")
		}
	}

	return proxy, nil
}

func defaultPort(kind string) int {
	switch kind {
	case "socks5":
		return 1080
	case "ss":
		return 8388
	default:
		return 443
	}
}

func urlPort(u *url.URL, fallback int) (int, error) {
	if s := u.Port(); s != "" {
		port, err := strconv.Atoi(s)
		if err != nil || port < 1 || port > 65535 {
			return 0, fmt.Errorf("invalid port %q", s)
		}
		return port, nil
	}
	return fallback, nil
}

func queryList(q url.Values, key string) []string {
	var result []string
	for _, value := range q[key] {
		result = append(result, splitCSV(value)...)
	}
	return result
}
