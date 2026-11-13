package surgefile

import (
	"embed"
	"fmt"
	"strings"
)

// The rule files are a snapshot of the active Surge profile's local RULE-SET
// files, copied here so the generated .conf does not depend on paths under
// ~/Library/Application Support/Surge. RULE-SET lines are expanded inline at
// build time; the policy is appended to each rule (before no-resolve and other
// options), preserving the original order.
//
//go:embed rulesets/*.list
var ruleSetFiles embed.FS

const (
	policyDirect       = "🚀直接连接"
	policyCN           = "🇨🇳国产AI"
	policyForeignAI    = "🤖国外AI"
	policySocial       = "💬社媒"
	policyTelegram     = "✈️Telegram"
	policyYouTube      = "🎬Youtube"
	policyNetflix      = "🎬Netflix"
	policyForeignMedia = "🎬国外媒体"
	policyBilibili     = "🎬哔哩哔哩"
	policyApple        = "🍎苹果服务"
	policyForeign      = "🔰国外流量"
	policyOther        = "⚓️其他流量"
)

// Rule group IDs used by the UI, CLI and HTTP API.
const (
	GroupCNAI         = "cn-ai"
	GroupForeignAI    = "foreign-ai"
	GroupSocial       = "social"
	GroupTelegram     = "telegram"
	GroupYouTube      = "youtube"
	GroupNetflix      = "netflix"
	GroupForeignMedia = "foreign-media"
	GroupBilibili     = "bilibili"
	GroupApple        = "apple"
	GroupForeign      = "foreign"
	GroupDirect       = "direct"
	GroupOther        = "other"
)

// RuleGroup is one selectable policy group.
type RuleGroup struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Default     bool   `json:"default"`
}

var ruleGroupDefs = []RuleGroup{
	{GroupCNAI, "🇨🇳 国产 AI", "国产 AI 直连", true},
	{GroupForeignAI, "🤖 国外 AI", "OpenAI / Claude / Gemini 等", true},
	{GroupSocial, "💬 社媒", "X / Discord / TikTok 等", true},
	{GroupTelegram, "✈️ Telegram", "Telegram 域名与 IP", true},
	{GroupYouTube, "🎬 YouTube", "YouTube / Google Video", true},
	{GroupNetflix, "🎬 Netflix", "Netflix 域名与 IP", true},
	{GroupForeignMedia, "🎬 国外媒体", "Spotify / Twitch 等", true},
	{GroupBilibili, "🎬 哔哩哔哩", "哔哩哔哩直连", true},
	{GroupApple, "🍎 苹果服务", "Apple 服务直连", true},
	{GroupForeign, "🔰 国外流量", "基础代理组：所有节点", true},
	{GroupDirect, "🚀 直接连接", "基础直连组 + 局域网规则", true},
	{GroupOther, "⚓️ 其他流量", "FINAL 兜底组", true},
}

var ruleSetDefs = []struct {
	id     string
	file   string
	policy string
}{
	{GroupCNAI, "cn-ai.list", policyCN},
	{GroupForeignAI, "foreign-ai.list", policyForeignAI},
	{GroupSocial, "social.list", policySocial},
	{GroupTelegram, "telegram.list", policyTelegram},
	{GroupYouTube, "youtube.list", policyYouTube},
	{GroupNetflix, "netflix.list", policyNetflix},
	{GroupForeignMedia, "foreign-media.list", policyForeignMedia},
	{GroupBilibili, "bilibili.list", policyBilibili},
	{GroupApple, "apple.list", policyApple},
	{GroupForeign, "foreign.list", policyForeign},
	{GroupDirect, "direct.list", policyDirect},
}

// Selection is a set of selected rule group IDs.
type Selection map[string]bool

// AvailableRuleGroups returns the selectable groups for the UI. A group's
// Default field controls whether it starts checked.
func AvailableRuleGroups() []RuleGroup {
	return append([]RuleGroup(nil), ruleGroupDefs...)
}

// AllRuleGroups selects every group, the historical default.
func AllRuleGroups() Selection {
	selection := make(Selection, len(ruleGroupDefs))
	for _, group := range ruleGroupDefs {
		selection[group.ID] = true
	}
	return selection
}

// ValidateSelection checks a comma separated group specification. "all",
// "none" and an empty string are valid.
func ValidateSelection(spec string) error {
	tokens, all, none := selectionTokens(spec)
	if all || none {
		return nil
	}
	known := make(map[string]bool, len(ruleGroupDefs))
	for _, group := range ruleGroupDefs {
		known[group.ID] = true
	}
	for _, id := range tokens {
		if !known[id] {
			return fmt.Errorf("unknown rule group %q", id)
		}
	}
	return nil
}

// ParseSelection turns "all", "none" or "cn-ai,telegram" into a Selection.
// An empty spec means all, preserving the old default.
func ParseSelection(spec string) Selection {
	tokens, all, none := selectionTokens(spec)
	if all || none {
		if none {
			return Selection{}
		}
		return AllRuleGroups()
	}
	selection := make(Selection, len(tokens))
	for _, id := range tokens {
		selection[id] = true
	}
	return selection
}

func selectionTokens(spec string) (tokens []string, all, none bool) {
	spec = strings.TrimSpace(strings.ToLower(spec))
	switch spec {
	case "", "all":
		return nil, true, false
	case "none":
		return nil, false, true
	}
	for _, part := range strings.Split(spec, ",") {
		if part = strings.TrimSpace(part); part != "" {
			tokens = append(tokens, part)
		}
	}
	if len(tokens) == 0 {
		return nil, false, true
	}
	return tokens, false, false
}

// DefaultRules is the fully expanded rule list with every group selected.
// It is a snapshot: to refresh it, replace internal/surgefile/rulesets/*.list
// and rebuild the app.
var DefaultRules = DefaultRulesFor(AllRuleGroups())

// DefaultRulesFor expands the selected rule sets plus the base rules.
func DefaultRulesFor(selection Selection) string {
	direct := target(policyDirect, "DIRECT", selection[GroupDirect])
	final := "DIRECT"
	if selection[GroupOther] {
		final = policyOther
	} else if selection[GroupForeign] {
		final = policyForeign
	}

	var b strings.Builder
	b.WriteString("# 默认规则来自当前 Surge 配置；RULE-SET 已展开为内联规则\n")
	b.WriteString("# PROCESS-NAME 规则仅 macOS 生效\n")
	fmt.Fprintf(&b, "PROCESS-NAME,CloudflareWARP,%s\n", direct)
	fmt.Fprintf(&b, "PROCESS-NAME,\"Cloudflare WARP\",%s\n", direct)

	// GEOIP rules must run after domain rules. Surge warns (and may issue a
	// DNS lookup) when an IP rule appears before a domain rule, so keep them
	// out of the expanded sets and append a single IP fallback section.
	var deferred []string
	seenDeferred := make(map[string]bool)
	for _, set := range ruleSetDefs {
		if !selection[set.id] {
			continue
		}
		fmt.Fprintf(&b, "\n# ---- %s ----\n", set.policy)
		content, err := ruleSetFiles.ReadFile("rulesets/" + set.file)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(content), "\n") {
			line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
				continue
			}
			expanded := applyPolicy(line, set.policy)
			if isGeoIPRule(expanded) {
				if !seenDeferred[expanded] {
					deferred = append(deferred, expanded)
					seenDeferred[expanded] = true
				}
				continue
			}
			b.WriteString(expanded)
			b.WriteByte('\n')
		}
	}

	b.WriteString("\n# ---- IP 兜底 ----\n")
	for _, line := range deferred {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	geoLine := fmt.Sprintf("GEOIP,CN,%s", direct)
	if !seenDeferred[geoLine] {
		b.WriteString(geoLine)
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "FINAL,%s\n", final)
	return b.String()
}

// DefaultProxyGroups adapts the source profile's groups to the generated node
// names, with every group selected.
func DefaultProxyGroups(names []string) []string {
	return DefaultProxyGroupsFor(names, AllRuleGroups())
}

// DefaultProxyGroupsFor defines only the selected groups, plus the groups they
// depend on. Unselected base groups are replaced by DIRECT or the node list in
// the remaining definitions.
func DefaultProxyGroupsFor(names []string, selection Selection) []string {
	if len(names) == 0 {
		return nil
	}
	joined := strings.Join(names, ", ")
	direct := target(policyDirect, "DIRECT", selection[GroupDirect])
	foreign := joined
	if selection[GroupForeign] {
		foreign = policyForeign
	}

	var groups []string
	if selection[GroupDirect] {
		groups = append(groups, fmt.Sprintf("%s = select, DIRECT", policyDirect))
	}
	if selection[GroupForeign] {
		groups = append(groups, fmt.Sprintf("%s = select, %s", policyForeign, joined))
	}

	categories := []struct {
		id     string
		policy string
		left   string
		right  string
	}{
		{GroupCNAI, policyCN, direct, foreign},
		{GroupForeignAI, policyForeignAI, foreign, direct},
		{GroupSocial, policySocial, foreign, direct},
		{GroupTelegram, policyTelegram, foreign, direct},
		{GroupYouTube, policyYouTube, foreign, direct},
		{GroupNetflix, policyNetflix, foreign, direct},
		{GroupForeignMedia, policyForeignMedia, foreign, direct},
		{GroupBilibili, policyBilibili, direct, foreign},
		{GroupApple, policyApple, direct, foreign},
	}
	for _, category := range categories {
		if selection[category.id] {
			groups = append(groups, fmt.Sprintf("%s = select, %s, %s", category.policy, category.left, category.right))
		}
	}

	if selection[GroupOther] {
		groups = append(groups, fmt.Sprintf("%s = select, %s, %s", policyOther, foreign, direct))
	}
	return groups
}

// RulesFor returns the selected expanded rules, or an empty string when rules
// are disabled.
func RulesFor(include bool, selection Selection) string {
	if !include {
		return ""
	}
	return DefaultRulesFor(selection)
}

// Rules is the compatibility helper that uses every group.
func Rules(include bool) string {
	return RulesFor(include, AllRuleGroups())
}

func target(policy, fallback string, selected bool) string {
	if selected {
		return policy
	}
	return fallback
}

func applyPolicy(line, policy string) string {
	fields := strings.Split(line, ",")
	if len(fields) < 2 {
		return line
	}

	insertAt := len(fields)
	for i := 2; i < len(fields); i++ {
		if isRuleOption(strings.TrimSpace(fields[i])) {
			insertAt = i
			break
		}
	}

	result := make([]string, 0, len(fields)+1)
	result = append(result, fields[:insertAt]...)
	result = append(result, policy)
	result = append(result, fields[insertAt:]...)
	return strings.Join(result, ",")
}

func isGeoIPRule(line string) bool {
	ruleType := strings.ToUpper(strings.TrimSpace(strings.SplitN(line, ",", 2)[0]))
	return strings.HasPrefix(ruleType, "GEOIP")
}

func isRuleOption(value string) bool {
	switch value {
	case "no-resolve", "extended-matching", "force-remote-dns", "pre-matching":
		return true
	default:
		return false
	}
}
