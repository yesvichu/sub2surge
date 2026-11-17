package parser

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/yesvichu/sub2surge/internal/model"
)

// looksLikeSurgeConfig reports whether the body is a Surge or Shadowrocket
// profile: a [Proxy] section with "name = type, host, port" lines.
func looksLikeSurgeConfig(text string) bool {
	if len(text) > 1<<20 {
		text = text[:1<<20]
	}
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "[Proxy]" || line == "[proxy]" {
			return true
		}
	}
	return false
}

// ParseSurgeConfig reads the [Proxy] section of a Surge or Shadowrocket
// profile. Those two share the proxy syntax, so one reader serves both.
func ParseSurgeConfig(body []byte) (*model.Result, error) {
	text := strings.TrimPrefix(string(body), "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")

	var (
		result   = &model.Result{}
		section  string
		seen     = make(map[string]bool)
		index    int
		sawProxy bool
	)

	for rawLine := range strings.SplitSeq(text, "\n") {
		index++
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			if section == "proxy" {
				sawProxy = true
			}
			continue
		}
		if section != "proxy" {
			continue
		}

		proxy, err := parseSurgeProxyLine(line)
		if err != nil {
			result.Skipped = append(result.Skipped, model.Skip{
				Line:   index,
				Scheme: strings.ToLower(surgeLineType(line)),
				Name:   surgeLineName(line),
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

	if !sawProxy {
		return nil, errors.New("no [Proxy] section found")
	}
	if len(result.Proxies) == 0 && len(result.Skipped) == 0 {
		return nil, errors.New("the [Proxy] section is empty")
	}
	return result, nil
}

// surgeLineName returns the part before "=" of a proxy line, for reporting.
func surgeLineName(line string) string {
	name, _, ok := strings.Cut(line, "=")
	if !ok {
		return ""
	}
	return strings.TrimSpace(name)
}

// surgeLineType returns the protocol of a proxy line, for reporting.
func surgeLineType(line string) string {
	_, rest, ok := strings.Cut(line, "=")
	if !ok {
		return ""
	}
	fields := splitFields(rest)
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimSpace(fields[0])
}

// parseSurgeProxyLine reads one "name = type, host, port, key=value..." line.
func parseSurgeProxyLine(line string) (model.Proxy, error) {
	name, rest, ok := strings.Cut(line, "=")
	if !ok {
		return model.Proxy{}, errors.New("line is not a proxy definition")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return model.Proxy{}, errors.New("proxy has no name")
	}

	fields := splitFields(rest)
	if len(fields) < 3 {
		return model.Proxy{}, errors.New("proxy needs a type, host and port")
	}

	kind := strings.ToLower(strings.TrimSpace(fields[0]))
	server := strings.TrimSpace(fields[1])
	port, err := strconv.Atoi(strings.TrimSpace(fields[2]))
	if err != nil {
		return model.Proxy{}, fmt.Errorf("invalid port %q", strings.TrimSpace(fields[2]))
	}

	proxy := model.Proxy{Name: name, Server: server, Port: port}
	params := parseSurgeParams(fields[3:])

	switch kind {
	case "ss":
		proxy.Type = "ss"
		proxy.Cipher = firstNonEmpty(params["encrypt-method"], params["method"])
		proxy.Password = params["password"]
		proxy.UDP = surfBool(params["udp-relay"])
		proxy.TFO = surfBool(params["tfo"])
		if obfs := params["obfs"]; obfs != "" {
			proxy.Plugin = "obfs"
			proxy.PluginOpts = map[string]string{
				"obfs": obfs,
				"host": firstNonEmpty(params["obfs-host"], params["obfs-host"]),
				"path": params["obfs-uri"],
			}
		}
		if password := params["shadow-tls-password"]; password != "" {
			proxy.Plugin = "shadow-tls"
			proxy.PluginOpts = map[string]string{
				"password": password,
				"host":     params["shadow-tls-sni"],
				"version":  params["shadow-tls-version"],
			}
		}

	case "vmess":
		proxy.Type = "vmess"
		proxy.UUID = params["username"]
		proxy.Cipher = params["encrypt-method"]
		proxy.TLS = surfBool(params["tls"])
		proxy.UDP = surfBool(params["udp-relay"])
		proxy.TFO = surfBool(params["tfo"])
		proxy.Insecure = surfBool(params["skip-cert-verify"])
		proxy.SNI = params["sni"]
		proxy.ALPN = splitList(params["alpn"])
		if params["vmess-aead"] == "false" {
			proxy.AlterID = 1
		}
		surgeTransport(&proxy, params)

	case "vless":
		proxy.Type = "vless"
		proxy.UUID = params["username"]
		proxy.Flow = params["flow"]
		proxy.TLS = surfBool(params["tls"])
		proxy.UDP = surfBool(params["udp-relay"])
		proxy.TFO = surfBool(params["tfo"])
		proxy.Insecure = surfBool(params["skip-cert-verify"])
		proxy.SNI = params["sni"]
		proxy.ALPN = splitList(params["alpn"])
		if key := params["reality-public-key"]; key != "" {
			proxy.Reality = true
			proxy.RealityPublicKey = key
			proxy.RealityShortID = params["reality-short-id"]
		}
		surgeTransport(&proxy, params)

	case "trojan":
		proxy.Type = "trojan"
		proxy.Password = params["password"]
		proxy.UDP = surfBool(params["udp-relay"])
		proxy.TFO = surfBool(params["tfo"])
		proxy.Insecure = surfBool(params["skip-cert-verify"])
		proxy.SNI = params["sni"]
		proxy.ALPN = splitList(params["alpn"])
		surgeTransport(&proxy, params)

	case "hysteria2", "hy2":
		proxy.Type = "hysteria2"
		proxy.Password = params["password"]
		proxy.TFO = surfBool(params["tfo"])
		proxy.Insecure = surfBool(params["skip-cert-verify"])
		proxy.SNI = params["sni"]
		proxy.Ports = params["port-hopping"]
		proxy.HopInterval = params["port-hopping-interval"]
		proxy.DownloadMbps = params["download-bandwidth"]
		if password := params["salamander-password"]; password != "" {
			proxy.Obfs = "salamander"
			proxy.ObfsPassword = password
		}
		if password := params["gecko-password"]; password != "" {
			proxy.Obfs = "gecko"
			proxy.ObfsPassword = password
		}

	case "tuic", "tuic-v5":
		proxy.Type = "tuic"
		proxy.Token = params["token"]
		if proxy.Token == "" {
			proxy.UUID = params["uuid"]
			proxy.Password = params["password"]
		}
		proxy.TFO = surfBool(params["tfo"])
		proxy.Insecure = surfBool(params["skip-cert-verify"])
		proxy.SNI = params["sni"]
		proxy.ALPN = splitList(params["alpn"])

	case "anytls":
		proxy.Type = "anytls"
		proxy.Password = params["password"]
		proxy.TFO = surfBool(params["tfo"])
		proxy.Insecure = surfBool(params["skip-cert-verify"])
		proxy.SNI = params["sni"]

	case "socks5", "socks5-tls":
		proxy.Type = "socks5"
		proxy.Username = params["username"]
		proxy.Password = params["password"]
		proxy.UDP = surfBool(params["udp-relay"])
		proxy.Insecure = surfBool(params["skip-cert-verify"])
		proxy.SNI = params["sni"]
		proxy.TLS = kind == "socks5-tls" || surfBool(params["tls"])

	case "ssr", "snell", "http", "https", "wireguard", "vless+reality":
		return model.Proxy{}, unsupported(kind, name,
			fmt.Sprintf("proxy type %q is not supported by this converter", kind))

	default:
		return model.Proxy{}, unsupported(kind, name,
			fmt.Sprintf("proxy type %q is not supported", kind))
	}

	if proxy.Server == "" || proxy.Port < 1 || proxy.Port > 65535 {
		return model.Proxy{}, fmt.Errorf("invalid endpoint %q:%d", proxy.Server, proxy.Port)
	}
	return proxy, nil
}

// surgeTransport fills the WebSocket fields both vmess and vless carry.
func surgeTransport(proxy *model.Proxy, params map[string]string) {
	if surfBool(params["ws"]) {
		proxy.Network = "ws"
		proxy.WSPath = params["ws-path"]
		if headers := params["ws-headers"]; headers != "" {
			proxy.WSHeaders = map[string]string{}
			for header := range strings.SplitSeq(headers, "|") {
				key, value, ok := strings.Cut(header, ":")
				if !ok {
					continue
				}
				if key = strings.TrimSpace(key); key != "" {
					proxy.WSHeaders[key] = strings.TrimSpace(value)
				}
			}
		}
	}
	if surfBool(params["grpc"]) {
		proxy.Network = "grpc"
		proxy.GRPCServiceName = params["grpc-service-name"]
	}
}

// parseSurgeParams turns "key=value" fields into a map. A bare key is true.
func parseSurgeParams(fields []string) map[string]string {
	params := make(map[string]string, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			params[strings.ToLower(key)] = "true"
			continue
		}
		params[strings.ToLower(strings.TrimSpace(key))] = unquoteValue(strings.TrimSpace(value))
	}
	return params
}

// splitFields splits on commas outside double quotes, so a quoted password or
// header list survives.
func splitFields(line string) []string {
	var (
		fields  []string
		current strings.Builder
		quoted  bool
	)
	for _, r := range line {
		switch {
		case r == '"':
			quoted = !quoted
			current.WriteRune(r)
		case r == ',' && !quoted:
			fields = append(fields, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	if rest := strings.TrimSpace(current.String()); rest != "" {
		fields = append(fields, rest)
	}
	return fields
}

// unquoteValue strips the surrounding quotes of a parameter value.
func unquoteValue(value string) string {
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		inner := value[1 : len(value)-1]
		if unescaped, err := strconv.Unquote(`"` + inner + `"`); err == nil {
			return unescaped
		}
		return inner
	}
	return value
}

// surfBool reads the booleans Surge configs use, where an absent key is false.
func surfBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "yes", "1", "on":
		return true
	}
	return false
}

// splitList splits a comma separated parameter, such as alpn.
func splitList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var out []string
	for part := range strings.SplitSeq(value, ",") {
		if text := strings.TrimSpace(part); text != "" {
			out = append(out, text)
		}
	}
	return out
}
