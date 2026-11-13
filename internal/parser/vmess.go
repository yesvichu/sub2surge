package parser

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/yesvichu/sub2surge/internal/model"
)

// parseVMess handles the standard v2rayN payload:
//
//	vmess://base64({"v":"2","ps":"name","add":"host","port":"443",...})
//
// It also accepts the URI-shaped form vmess://uuid@host:port?... used by a few
// clients.
func parseVMess(line string) (model.Proxy, error) {
	rest := strings.TrimPrefix(line, "vmess://")
	body, fragment := cutFragment(rest)

	if strings.Contains(body, "@") {
		proxy, err := parseURLNode(line, "vmess")
		if err != nil {
			return model.Proxy{}, err
		}
		if proxy.UUID == "" {
			return model.Proxy{}, fmt.Errorf("vmess: missing UUID")
		}
		return proxy, nil
	}

	// A handful of providers append a query string to the base64 blob.
	if i := strings.IndexByte(body, '?'); i >= 0 {
		body = body[:i]
	}

	decoded, ok := decodeBase64Text(body)
	if !ok {
		if unescaped, err := url.PathUnescape(body); err == nil {
			decoded, ok = unescaped, strings.HasPrefix(strings.TrimSpace(unescaped), "{")
		}
	}
	if !ok {
		return model.Proxy{}, fmt.Errorf("vmess: payload is not valid base64")
	}
	decoded = strings.TrimSpace(decoded)
	if strings.HasPrefix(decoded, "vmess=") || strings.Contains(decoded, "=vmess,") {
		return model.Proxy{}, fmt.Errorf("vmess: Quantumult-style vmess link is not supported; use a standard v2rayN vmess:// link")
	}

	decoder := json.NewDecoder(strings.NewReader(decoded))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return model.Proxy{}, fmt.Errorf("vmess: payload is not JSON: %w", err)
	}

	server := strings.TrimSpace(anyString(raw["add"]))
	port := anyInt(raw["port"])
	uuid := strings.TrimSpace(anyString(raw["id"]))
	if server == "" {
		return model.Proxy{}, fmt.Errorf("vmess: missing server (add)")
	}
	if port < 1 || port > 65535 {
		return model.Proxy{}, fmt.Errorf("vmess: invalid port %q", anyString(raw["port"]))
	}
	if uuid == "" {
		return model.Proxy{}, fmt.Errorf("vmess: missing UUID (id)")
	}

	name := firstNonEmpty(
		fragment,
		anyString(raw["ps"]),
		anyString(raw["remarks"]),
		anyString(raw["remark"]),
		defaultName("vmess", server, port),
	)

	proxy := model.Proxy{
		Name:    name,
		Type:    "vmess",
		Server:  server,
		Port:    port,
		UUID:    uuid,
		Cipher:  strings.ToLower(strings.TrimSpace(anyString(raw["scy"]))),
		AlterID: anyInt(raw["aid"]),
	}
	if proxy.Cipher == "" {
		proxy.Cipher = "auto"
	}
	if proxy.AlterID == 0 {
		proxy.AlterID = anyInt(raw["alterId"])
	}

	proxy.TLS = isTruthy(raw["tls"]) || isTruthy(raw["security"])
	if value, exists := raw["verify_cert"]; exists && !isTruthy(value) {
		proxy.Insecure = true
	}
	if isTruthy(raw["allowInsecure"]) {
		proxy.Insecure = true
	}
	proxy.SNI = firstNonEmpty(anyString(raw["sni"]), anyString(raw["peer"]))
	proxy.ALPN = anyStringList(raw["alpn"])

	network := strings.ToLower(strings.TrimSpace(anyString(raw["net"])))
	if network == "" {
		network = "tcp"
	}
	headerType := strings.ToLower(strings.TrimSpace(anyString(raw["type"])))
	hostHeader := wsHost(anyString(raw["host"]))

	switch network {
	case "ws", "websocket":
		proxy.Network = "ws"
		proxy.WSPath = cleanWSPath(anyString(raw["path"]))
		if hostHeader != "" {
			proxy.WSHeaders = map[string]string{"Host": hostHeader}
		}
	case "grpc":
		proxy.Network = "grpc"
		proxy.GRPCServiceName = firstNonEmpty(
			anyString(raw["path"]),
			anyString(raw["serviceName"]),
		)
	case "http", "h2":
		proxy.Network = "h2"
	case "httpupgrade":
		proxy.Network = "httpupgrade"
	case "tcp", "raw", "none":
		if headerType == "http" {
			proxy.Network = "http"
		} else {
			proxy.Network = "tcp"
		}
	case "kcp", "quic":
		proxy.Network = network
	default:
		proxy.Network = network
	}

	if proxy.TLS && proxy.SNI == "" {
		proxy.SNI = hostHeader
	}

	if proxy.AlterID < 0 {
		proxy.AlterID = 0
	}
	return proxy, nil
}
