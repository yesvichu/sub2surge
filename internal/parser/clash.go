package parser

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/yesvichu/sub2surge/internal/model"
)

// looksLikeClash reports whether the body is a Clash-style YAML profile. Only a
// top-level "proxies:" counts, since nested keys are indented.
func looksLikeClash(text string) bool {
	for line := range strings.SplitSeq(text, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// No trimming: an indented "proxies:" belongs to a nested mapping,
		// such as a proxy-provider's override.
		if line == "proxies:" || strings.HasPrefix(line, "proxies: ") {
			return true
		}
	}
	return false
}

// ParseClash turns a Clash/mihomo YAML profile into canonical proxies. Nodes
// Surge cannot express are reported as skips rather than dropped silently.
func ParseClash(body []byte) (*model.Result, error) {
	text := strings.TrimPrefix(string(body), "\ufeff")

	// A profile that is one JSON document is a sing-box config, not Clash.
	if strings.HasPrefix(strings.TrimSpace(text), "{") {
		return nil, errors.New("input is a sing-box JSON config, which this tool cannot read")
	}

	proxies, err := clashProxies([]byte(text))
	if err != nil {
		return nil, err
	}

	result := &model.Result{}
	seen := make(map[string]bool)
	for index, raw := range proxies {
		kind := strings.ToLower(clashString(raw, "type"))
		name := clashString(raw, "name")

		proxy, err := clashProxy(raw)
		if err != nil {
			result.Skipped = append(result.Skipped, model.Skip{
				Line:   index + 1,
				Scheme: kind,
				Name:   name,
				Reason: err.Error(),
			})
			continue
		}
		key := dedupeKey(proxy)
		if seen[key] {
			continue
		}
		seen[key] = true
		result.Proxies = append(result.Proxies, proxy)
	}

	if len(result.Proxies) == 0 && len(result.Skipped) == 0 {
		return nil, errors.New("no proxy nodes in the Clash profile")
	}
	return result, nil
}

// clashProxies reads the top-level proxies list.
func clashProxies(body []byte) ([]map[string]any, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(body, &document); err != nil {
		return nil, fmt.Errorf("parse Clash YAML: %w", err)
	}

	root := &document
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil, errors.New("the Clash profile is not a YAML mapping")
	}

	var list *yaml.Node
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "proxies" {
			list = root.Content[i+1]
			break
		}
	}
	if list == nil || list.Kind != yaml.SequenceNode || len(list.Content) == 0 {
		return nil, errors.New("no proxies list in the Clash profile")
	}

	proxies := make([]map[string]any, 0, len(list.Content))
	for _, item := range list.Content {
		proxies = append(proxies, clashNodeMap(item))
	}
	return proxies, nil
}

// clashNodeMap turns a mapping node into a map whose scalars are the text as
// written, so a quoted password and a leading-zero ID both survive.
func clashNodeMap(node *yaml.Node) map[string]any {
	out := make(map[string]any)
	node = resolveAlias(node)
	if node.Kind != yaml.MappingNode {
		return out
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		out[node.Content[i].Value] = clashNodeValue(node.Content[i+1])
	}
	return out
}

// clashNodeValue converts a node to a scalar string, a slice or a map.
func clashNodeValue(node *yaml.Node) any {
	node = resolveAlias(node)
	switch node.Kind {
	case yaml.ScalarNode:
		return node.Value
	case yaml.SequenceNode:
		items := make([]any, 0, len(node.Content))
		for _, item := range node.Content {
			items = append(items, clashNodeValue(item))
		}
		return items
	case yaml.MappingNode:
		return clashNodeMap(node)
	}
	return node.Value
}

// resolveAlias follows an anchor reference, as proxy lists sometimes reuse a
// block such as shared ws-opts.
func resolveAlias(node *yaml.Node) *yaml.Node {
	for node != nil && node.Kind == yaml.AliasNode && node.Alias != nil {
		node = node.Alias
	}
	return node
}

// clashProxy maps one Clash proxy entry onto the canonical model.
func clashProxy(raw map[string]any) (model.Proxy, error) {
	kind := strings.ToLower(clashString(raw, "type"))
	name := clashString(raw, "name")

	proxy := model.Proxy{
		Name:        name,
		Server:      clashString(raw, "server"),
		Port:        clashInt(raw, "port"),
		UDP:         clashBool(raw, "udp"),
		TFO:         clashBool(raw, "tfo"),
		Insecure:    clashBool(raw, "skip-cert-verify"),
		SNI:         clashFirstString(raw, "servername", "sni", "server-name"),
		ALPN:        clashStrings(raw, "alpn"),
		Fingerprint: clashString(raw, "client-fingerprint"),
	}
	if proxy.Name == "" {
		return model.Proxy{}, errors.New("node has no name")
	}
	if proxy.Server == "" || proxy.Port < 1 || proxy.Port > 65535 {
		return model.Proxy{}, fmt.Errorf("node has an invalid endpoint %q:%d", proxy.Server, proxy.Port)
	}

	switch kind {
	case "ss", "shadowsocks":
		proxy.Type = "ss"
		proxy.Cipher = clashString(raw, "cipher")
		proxy.Password = clashString(raw, "password")
		plugin, err := clashSSPlugin(raw)
		if err != nil {
			return model.Proxy{}, err
		}
		proxy.Plugin = plugin.name
		proxy.PluginOpts = plugin.opts

	case "vmess":
		proxy.Type = "vmess"
		proxy.UUID = clashString(raw, "uuid")
		proxy.AlterID = clashInt(raw, "alterId", "alterid", "aid")
		proxy.Cipher = clashString(raw, "cipher")
		proxy.Network = clashNetwork(clashString(raw, "network"))
		proxy.TLS = clashBool(raw, "tls")
		clashTransport(raw, &proxy)

	case "vless":
		proxy.Type = "vless"
		proxy.UUID = clashString(raw, "uuid")
		proxy.Flow = clashString(raw, "flow")
		proxy.Network = clashNetwork(clashString(raw, "network"))
		proxy.TLS = clashBool(raw, "tls")
		clashTransport(raw, &proxy)
		if reality := clashMap(raw, "reality-opts"); len(reality) > 0 {
			proxy.Reality = true
			proxy.RealityPublicKey = clashString(reality, "public-key")
			proxy.RealityShortID = clashString(reality, "short-id")
		}

	case "trojan":
		proxy.Type = "trojan"
		proxy.Password = clashString(raw, "password")
		proxy.Network = clashNetwork(clashString(raw, "network"))
		clashTransport(raw, &proxy)

	case "hysteria2", "hy2":
		proxy.Type = "hysteria2"
		proxy.Password = clashString(raw, "password", "auth")
		proxy.Obfs = clashString(raw, "obfs")
		proxy.ObfsPassword = clashString(raw, "obfs-password")
		proxy.Ports = clashString(raw, "ports")
		proxy.HopInterval = clashString(raw, "hop-interval")
		// Clash states bandwidth in Mbps, which is what Surge takes.
		proxy.DownloadMbps = clashString(raw, "down", "download-bandwidth")

	case "tuic":
		proxy.Type = "tuic"
		proxy.UUID = clashString(raw, "uuid")
		proxy.Password = clashString(raw, "password")
		proxy.Token = clashString(raw, "token")
		proxy.CongestionControl = clashString(raw, "congestion-controller")
		proxy.UDPRelayMode = clashString(raw, "udp-relay-mode")

	case "anytls":
		proxy.Type = "anytls"
		proxy.Password = clashString(raw, "password")

	case "socks5":
		proxy.Type = "socks5"
		proxy.Username = clashString(raw, "username")
		proxy.Password = clashString(raw, "password")
		proxy.TLS = clashBool(raw, "tls")

	case "ssr":
		return model.Proxy{}, unsupported("ssr", name,
			"Surge does not support SSR (only SurgeMac via a mihomo external proxy)")
	case "snell":
		return model.Proxy{}, unsupported("snell", name,
			"Snell is not supported by this converter")
	case "http", "https":
		return model.Proxy{}, unsupported(kind, name,
			"HTTP proxies are not supported by this converter")
	case "wireguard", "ssh", "hysteria", "mieru", "juicity", "direct", "reject", "reject-drop", "pass", "dns":
		return model.Proxy{}, unsupported(kind, name,
			fmt.Sprintf("Clash node type %q is not a proxy Surge can express", kind))
	case "":
		return model.Proxy{}, errors.New("node has no type")
	default:
		return model.Proxy{}, unsupported(kind, name,
			fmt.Sprintf("Clash node type %q is not supported", kind))
	}

	return proxy, nil
}

type ssPlugin struct {
	name string
	opts map[string]string
}

// clashSSPlugin maps a Clash Shadowsocks plugin onto what the Surge producer
// can write.
func clashSSPlugin(raw map[string]any) (ssPlugin, error) {
	name := strings.ToLower(clashString(raw, "plugin"))
	opts := clashMap(raw, "plugin-opts")

	switch name {
	case "", "none":
		return ssPlugin{}, nil
	case "obfs", "simple-obfs", "obfs-local":
		mode := firstNonEmpty(clashString(opts, "mode"), "http")
		return ssPlugin{name: "obfs", opts: map[string]string{
			"obfs": mode,
			"host": clashString(opts, "host"),
		}}, nil
	case "shadow-tls":
		return ssPlugin{name: "shadow-tls", opts: map[string]string{
			"host":     clashString(opts, "host"),
			"password": clashString(opts, "password"),
			"version":  clashString(opts, "version"),
		}}, nil
	default:
		// v2ray-plugin and gost-plugin have no Surge equivalent.
		return ssPlugin{}, unsupported(name, clashString(raw, "name"),
			fmt.Sprintf("Shadowsocks plugin %q has no Surge equivalent", name))
	}
}

// clashTransport fills the WebSocket or gRPC fields of a node.
func clashTransport(raw map[string]any, proxy *model.Proxy) {
	switch proxy.Network {
	case "ws":
		if opts := clashMap(raw, "ws-opts"); len(opts) > 0 {
			proxy.WSPath = clashString(opts, "path")
			if headers := clashMap(opts, "headers"); len(headers) > 0 {
				proxy.WSHeaders = make(map[string]string, len(headers))
				for key, value := range headers {
					proxy.WSHeaders[key] = clashValueString(value)
				}
			}
		}
	case "grpc":
		if opts := clashMap(raw, "grpc-opts"); len(opts) > 0 {
			proxy.GRPCServiceName = clashString(opts, "grpc-service-name")
		}
	}
}

// clashNetwork normalizes the transport names Clash uses.
func clashNetwork(network string) string {
	switch strings.ToLower(strings.TrimSpace(network)) {
	case "", "tcp", "raw":
		return ""
	case "ws", "websocket":
		return "ws"
	case "grpc":
		return "grpc"
	case "h2", "http":
		return "h2"
	case "httpupgrade":
		return "httpupgrade"
	case "quic", "kcp", "mkcp":
		return "quic"
	}
	return strings.ToLower(strings.TrimSpace(network))
}

// clashValueString renders a YAML scalar as a string.
func clashValueString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return fmt.Sprint(value)
}

// clashString returns the first of keys that is set.
func clashString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key]; ok {
			if text := strings.TrimSpace(clashValueString(value)); text != "" {
				return text
			}
		}
	}
	return ""
}

// clashFirstString is clashString for a nested map.
func clashFirstString(m map[string]any, keys ...string) string {
	return clashString(m, keys...)
}

// clashInt reads an integer given as a number or a string.
func clashInt(m map[string]any, keys ...string) int {
	for _, key := range keys {
		value, ok := m[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		case string:
			if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				return parsed
			}
		}
	}
	return 0
}

// clashBool reads a boolean given as a bool, a number or a string.
func clashBool(m map[string]any, keys ...string) bool {
	for _, key := range keys {
		value, ok := m[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case bool:
			return v
		case int:
			return v != 0
		case string:
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "true", "yes", "1", "on":
				return true
			case "false", "no", "0", "off", "":
				return false
			}
		}
	}
	return false
}

// clashStrings reads a list of strings, also accepting a comma separated one.
func clashStrings(m map[string]any, keys ...string) []string {
	for _, key := range keys {
		value, ok := m[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case []any:
			out := make([]string, 0, len(v))
			for _, item := range v {
				if text := strings.TrimSpace(clashValueString(item)); text != "" {
					out = append(out, text)
				}
			}
			return out
		case string:
			var out []string
			for part := range strings.SplitSeq(v, ",") {
				if text := strings.TrimSpace(part); text != "" {
					out = append(out, text)
				}
			}
			return out
		}
	}
	return nil
}

// clashMap reads a nested mapping, such as ws-opts.
func clashMap(m map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if nested, ok := m[key].(map[string]any); ok {
			return nested
		}
	}
	return nil
}
