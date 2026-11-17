package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yesvichu/sub2surge/internal/model"
)

// parseSSR only extracts enough information to report a useful skip reason.
// Surge has no native SSR support (Sub-Store solves this with SurgeMac +
// mihomo external), so a full canonical model would not be usable here.
func parseSSR(line string) (model.Proxy, error) {
	rest := strings.TrimPrefix(line, "ssr://")
	decoded, ok := decodeBase64Text(rest)
	if !ok {
		return model.Proxy{}, unsupported("ssr", fragmentName(line), "Surge does not support SSR (only SurgeMac via mihomo external)")
	}

	main, _, _ := strings.Cut(decoded, "/?")
	parts := strings.SplitN(main, ":", 6)
	if len(parts) < 6 {
		return model.Proxy{}, unsupported("ssr", fragmentName(line), "Surge does not support SSR (only SurgeMac via mihomo external)")
	}

	port, _ := strconv.Atoi(parts[1])
	name := ""
	if _, after, ok := strings.Cut(decoded, "remarks="); ok {
		remarks := after
		if amp := strings.IndexByte(remarks, '&'); amp >= 0 {
			remarks = remarks[:amp]
		}
		if decodedRemarks, ok := decodeBase64Text(remarks); ok {
			name = strings.TrimSpace(decodedRemarks)
		}
	}
	if name == "" {
		name = fmt.Sprintf("SSR %s:%d", parts[0], port)
	}
	return model.Proxy{}, unsupported("ssr", name, "Surge does not support SSR (only SurgeMac via mihomo external)")
}
