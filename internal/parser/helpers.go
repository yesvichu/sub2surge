package parser

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yesvichu/sub2surge/internal/model"
)

// decodeBase64Text decodes standard or URL-safe base64, with or without
// padding and with arbitrary whitespace. It only returns printable text so a
// random binary blob is not mistaken for a subscription.
func decodeBase64Text(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	s = strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\r', '\n':
			return -1
		}
		return r
	}, s)

	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, enc := range encodings {
		raw, err := enc.DecodeString(s)
		if err != nil || !utf8.Valid(raw) {
			continue
		}
		text := string(raw)
		if isPrintableText(text) {
			return text, true
		}
	}
	return "", false
}

func isPrintableText(s string) bool {
	if s == "" {
		return false
	}
	printable, total := 0, 0
	for _, r := range s {
		total++
		switch r {
		case '\n', '\r', '\t':
			printable++
			continue
		}
		if r == 0 || !unicode.IsPrint(r) {
			continue
		}
		printable++
	}
	return total > 0 && float64(printable)/float64(total) > 0.95
}

// cutFragment splits "#name" off a URI body and percent-decodes the name.
func cutFragment(s string) (body, fragment string) {
	if i := strings.IndexByte(s, '#'); i >= 0 {
		return s[:i], decodeComponent(s[i+1:])
	}
	return s, ""
}

func decodeComponent(s string) string {
	if v, err := url.PathUnescape(s); err == nil {
		return v
	}
	return s
}

func splitHostPort(s string) (string, int, error) {
	s = strings.TrimSpace(strings.TrimSuffix(s, "/"))
	if s == "" {
		return "", 0, fmt.Errorf("empty host:port")
	}
	if host, portStr, err := net.SplitHostPort(s); err == nil {
		port, convErr := strconv.Atoi(portStr)
		if convErr != nil || port < 1 || port > 65535 {
			return "", 0, fmt.Errorf("invalid port %q", portStr)
		}
		return strings.Trim(host, "[]"), port, nil
	}
	return "", 0, fmt.Errorf("invalid host:port %q", s)
}

// fullUserinfo returns the decoded userinfo as a single string. For
// "password:with:colons" net/url splits at the first colon; joining restores it.
func fullUserinfo(u *url.Userinfo) string {
	if u == nil {
		return ""
	}
	if password, ok := u.Password(); ok {
		return u.Username() + ":" + password
	}
	return u.Username()
}

func queryBool(q url.Values, keys ...string) bool {
	for _, key := range keys {
		values, ok := q[key]
		if !ok || len(values) == 0 {
			continue
		}
		value := strings.TrimSpace(values[0])
		if value == "" {
			return true
		}
		switch strings.ToLower(value) {
		case "1", "true", "yes", "on", "tls":
			return true
		case "0", "false", "no", "off", "none":
			return false
		}
	}
	return false
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func anyString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	default:
		return fmt.Sprint(x)
	}
}

func anyInt(v any) int {
	switch x := v.(type) {
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return int(n)
		}
		if f, err := x.Float64(); err == nil {
			return int(f)
		}
	case float64:
		return int(x)
	}
	text := strings.TrimSpace(anyString(v))
	if n, err := strconv.Atoi(text); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(text, 64); err == nil {
		return int(f)
	}
	return 0
}

func isTruthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case json.Number:
		f, _ := x.Float64()
		return f != 0
	case float64:
		return x != 0
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "1", "true", "yes", "on", "tls", "reality":
			return true
		}
	}
	return false
}

func anyStringList(v any) []string {
	switch x := v.(type) {
	case nil:
		return nil
	case []any:
		result := make([]string, 0, len(x))
		for _, item := range x {
			if s := strings.TrimSpace(anyString(item)); s != "" {
				result = append(result, s)
			}
		}
		return result
	case []string:
		return x
	case string:
		return splitCSV(x)
	default:
		return splitCSV(anyString(x))
	}
}

func cleanWSPath(path string) string {
	if path == "" {
		return "/"
	}
	if i := strings.Index(path, "?ed="); i >= 0 {
		path = path[:i]
	}
	if path == "" {
		return "/"
	}
	return path
}

// wsHost accepts a plain host, a comma separated host list, or the JSON object
// some providers put in the "host" field.
func wsHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if strings.HasPrefix(host, "{") {
		var m map[string]any
		if err := json.Unmarshal([]byte(host), &m); err == nil {
			for key, value := range m {
				if strings.EqualFold(key, "host") {
					return wsHost(anyString(value))
				}
			}
		}
	}
	if i := strings.IndexByte(host, ','); i >= 0 {
		host = host[:i]
	}
	return strings.TrimSpace(host)
}

func schemeOf(line string) string {
	line = strings.TrimSpace(line)
	if i := strings.Index(line, "://"); i > 0 {
		return strings.ToLower(line[:i])
	}
	return ""
}

func fragmentName(line string) string {
	if i := strings.IndexByte(line, '#'); i >= 0 {
		return decodeComponent(line[i+1:])
	}
	return ""
}

func dedupeKey(p model.Proxy) string {
	return strings.Join([]string{
		p.Type,
		strings.ToLower(p.Server),
		strconv.Itoa(p.Port),
		p.UUID,
		p.Password,
		p.Network,
		p.WSPath,
	}, "\x00")
}

func defaultName(kind, server string, port int) string {
	switch kind {
	case "hysteria2":
		return fmt.Sprintf("Hysteria2 %s:%d", server, port)
	case "tuic":
		return fmt.Sprintf("TUIC %s:%d", server, port)
	case "anytls":
		return fmt.Sprintf("AnyTLS %s:%d", server, port)
	case "vmess":
		return fmt.Sprintf("VMess %s:%d", server, port)
	case "vless":
		return fmt.Sprintf("VLESS %s:%d", server, port)
	case "trojan":
		return fmt.Sprintf("Trojan %s:%d", server, port)
	case "socks5":
		return fmt.Sprintf("SOCKS5 %s:%d", server, port)
	default:
		return fmt.Sprintf("%s %s:%d", kind, server, port)
	}
}
