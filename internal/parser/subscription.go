package parser

import (
	"errors"
	"fmt"
	"strings"

	"github.com/yesvichu/sub2surge/internal/model"
)

// Parse turns a subscription body into canonical proxies.
//
// Accepted inputs:
//   - a base64 blob of proxy URIs (the usual v2rayN subscription body)
//   - one URI per line, optionally with comments
//   - a Clash/mihomo YAML profile, whose "proxies" list is read
//   - a Surge or Shadowrocket profile, whose [Proxy] section is read
//
// A Clash or Surge body can also be base64 encoded; the decoded form is
// detected the same way.
func Parse(body []byte) (*model.Result, error) {
	text := strings.TrimPrefix(string(body), "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("empty subscription body")
	}

	if result, ok, err := parseStructured(text); ok {
		return result, err
	}

	if !strings.Contains(text, "://") && !looksLikeSurgeConfig(text) {
		decoded, ok := decodeBase64Text(text)
		if !ok {
			return nil, errors.New("subscription is neither a proxy URI list nor valid base64; check the URL and the request User-Agent")
		}
		text = decoded
		if result, ok, err := parseStructured(text); ok {
			return result, err
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

// parseStructured handles bodies that are not URI lists. It reports ok=false
// when the body is a plain URI list, which the caller then reads line by line.
func parseStructured(text string) (*model.Result, bool, error) {
	if looksLikeSurgeConfig(text) {
		result, err := ParseSurgeConfig([]byte(text))
		return result, true, err
	}
	if looksLikeClash(text) {
		result, err := ParseClash([]byte(text))
		return result, true, err
	}
	if looksLikeStructuredConfig(text) {
		return nil, true, errors.New("input is a sing-box or other structured config this tool cannot read; pass a Clash YAML, a Surge/Shadowrocket profile, or a v2ray URI list")
	}
	return nil, false, nil
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

	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "proxy-groups:"),
			strings.HasPrefix(line, "rules:"),
			strings.HasPrefix(line, "rule-providers:"),
			strings.HasPrefix(line, "outbounds:"):
			return true
		}
	}
	return false
}
