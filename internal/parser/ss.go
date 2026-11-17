package parser

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/yesvichu/sub2surge/internal/model"
)

// parseSS supports SIP002 and the legacy fully-base64 encoded form.
func parseSS(line string) (model.Proxy, error) {
	rest := strings.TrimPrefix(line, "ss://")
	body, fragment := cutFragment(rest)

	var query string
	if i := strings.IndexByte(body, '?'); i >= 0 {
		query, body = body[i+1:], body[:i]
	}
	body = strings.TrimSuffix(body, "/")

	var method, password, hostPort string
	if at := strings.LastIndexByte(body, '@'); at >= 0 {
		userinfo := body[:at]
		hostPort = body[at+1:]

		if decoded, ok := decodeBase64Text(userinfo); ok && strings.Contains(decoded, ":") {
			method, password, _ = strings.Cut(decoded, ":")
		} else {
			userinfo = decodeComponent(userinfo)
			method, password, _ = strings.Cut(userinfo, ":")
		}
	} else {
		decoded, ok := decodeBase64Text(body)
		if !ok {
			return model.Proxy{}, fmt.Errorf("ss: invalid SIP002/legacy payload")
		}
		// Some legacy encoders put "#name" inside the base64 blob.
		if i := strings.IndexByte(decoded, '#'); i >= 0 {
			if fragment == "" {
				fragment = decodeComponent(decoded[i+1:])
			}
			decoded = decoded[:i]
		}
		if at := strings.LastIndexByte(decoded, '@'); at >= 0 {
			userinfo := decoded[:at]
			hostPort = decoded[at+1:]
			method, password, _ = strings.Cut(userinfo, ":")
		} else {
			return model.Proxy{}, fmt.Errorf("ss: missing server part")
		}
	}

	method = strings.ToLower(strings.TrimSpace(method))
	if method == "" {
		return model.Proxy{}, fmt.Errorf("ss: missing encryption method")
	}
	password = strings.TrimSpace(password)

	server, port, err := splitHostPort(hostPort)
	if err != nil {
		return model.Proxy{}, fmt.Errorf("ss: %w", err)
	}
	if server == "" {
		return model.Proxy{}, fmt.Errorf("ss: missing server")
	}

	name := firstNonEmpty(fragment, defaultName("ss", server, port))
	proxy := model.Proxy{
		Name:     name,
		Type:     "ss",
		Server:   server,
		Port:     port,
		Cipher:   method,
		Password: password,
	}

	if query != "" {
		// Do not use url.ParseQuery: providers frequently leave the ";" inside
		// plugin=obfs-local;obfs=http unencoded, which modern Go rejects.
		values := parseQueryLoose(query)
		if security := values["security"]; security != "" && !strings.EqualFold(security, "none") {
			// SS-over-TLS is not a Surge SS transport; the producer rejects it.
			proxy.TLS = true
		}
		if plugin := values["plugin"]; plugin != "" {
			parts := strings.Split(plugin, ";")
			proxy.Plugin = strings.TrimSpace(parts[0])
			proxy.PluginOpts = make(map[string]string)
			for _, item := range parts[1:] {
				key, value, _ := strings.Cut(item, "=")
				if key = strings.TrimSpace(key); key != "" {
					proxy.PluginOpts[key] = value
				}
			}
		}
	}

	return proxy, nil
}

func parseQueryLoose(raw string) map[string]string {
	values := make(map[string]string)
	for item := range strings.SplitSeq(raw, "&") {
		if item == "" {
			continue
		}
		key, value, _ := strings.Cut(item, "=")
		if decoded, err := url.QueryUnescape(key); err == nil {
			key = decoded
		}
		if decoded, err := url.QueryUnescape(value); err == nil {
			value = decoded
		}
		if key = strings.TrimSpace(key); key != "" {
			values[key] = value
		}
	}
	return values
}
