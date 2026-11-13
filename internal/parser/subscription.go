package parser

import (
	"errors"
	"fmt"
	"strings"

	"github.com/yesvichu/sub2surge/internal/model"
)

// Parse turns a v2ray-style subscription body into canonical proxies.
//
// Accepted inputs:
//   - a base64 blob of proxy URIs (the usual v2rayN/Clash "subscription" body)
//   - one URI per line, optionally with comments
//
// It deliberately does not accept Clash/mihomo YAML: converting that needs a
// full YAML parser and is better served by passing the original subscription
// URL, which usually also serves the URI form for v2ray User-Agents.
func Parse(body []byte) (*model.Result, error) {
	text := strings.TrimPrefix(string(body), "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("empty subscription body")
	}

	if looksLikeStructuredConfig(text) {
		return nil, errors.New("input looks like a Clash/sing-box config, not a v2ray URI subscription; pass the original subscription URL or ask the provider for a v2ray-format link")
	}

	if !strings.Contains(text, "://") {
		decoded, ok := decodeBase64Text(text)
		if !ok {
			return nil, errors.New("subscription is neither a proxy URI list nor valid base64; check the URL and the request User-Agent")
		}
		text = decoded
		if looksLikeStructuredConfig(text) {
			return nil, errors.New("decoded subscription is a Clash/sing-box config, not a v2ray URI list")
		}
	}
	if !strings.Contains(text, "://") {
		return nil, errors.New("decoded subscription contains no proxy URIs")
	}

	result := &model.Result{}
	seen := make(map[string]bool)

	for index, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || isComment(line) {
			continue
		}
		if !strings.Contains(line, "://") {
			// Providers sometimes prepend "STATUS=..." / "remarks=..." lines.
			continue
		}

		proxy, err := ParseURI(line)
		if err != nil {
			skip := model.Skip{
				Line:   index + 1,
				Scheme: schemeOf(line),
				Name:   fragmentName(line),
				Reason: err.Error(),
			}
			var unsupportedErr *UnsupportedError
			if errors.As(err, &unsupportedErr) {
				if unsupportedErr.Name != "" {
					skip.Name = unsupportedErr.Name
				}
				if unsupportedErr.Scheme != "" {
					skip.Scheme = unsupportedErr.Scheme
				}
			}
			result.Skipped = append(result.Skipped, skip)
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
		return nil, fmt.Errorf("no proxy URIs found in subscription (%d bytes)", len(body))
	}
	return result, nil
}

func isComment(line string) bool {
	return strings.HasPrefix(line, "#") ||
		strings.HasPrefix(line, "//") ||
		strings.HasPrefix(line, ";")
}

func looksLikeStructuredConfig(text string) bool {
	if len(text) > 1<<20 {
		text = text[:1<<20]
	}
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return strings.Contains(trimmed, `"outbounds"`) || strings.Contains(trimmed, `"proxies"`)
	}

	hasProxies := false
	hasConfigKey := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "proxies:"):
			hasProxies = true
		case strings.HasPrefix(line, "proxy-groups:"),
			strings.HasPrefix(line, "rules:"),
			strings.HasPrefix(line, "rule-providers:"),
			strings.HasPrefix(line, "outbounds:"):
			hasConfigKey = true
		}
	}
	return hasProxies && hasConfigKey
}
