package surgefile

import (
	"strings"
	"testing"

	"github.com/yesvichu/sub2surge/internal/model"
)

func TestRenderProfile(t *testing.T) {
	lines := []string{
		`HK=vmess,hk.example.com,443,username=uuid`,
		`JP=vless,jp.example.com,443,username=uuid,tls=true`,
	}
	names := []string{"HK", "JP"}

	profile := RenderProfile(lines, names, ProfileOptions{Title: "test"})
	for _, want := range []string{
		"# test",
		"[General]",
		"[Proxy]",
		"HK=vmess,hk.example.com,443,username=uuid",
		"[Rule]",
		"FINAL,DIRECT",
	} {
		if !strings.Contains(profile, want) {
			t.Fatalf("profile missing %q:\n%s", want, profile)
		}
	}
	if strings.Contains(profile, "#!MANAGED-CONFIG") {
		t.Fatalf("plain profile must not be managed:\n%s", profile)
	}

	withRules := RenderProfile(lines, names, ProfileOptions{
		Rules:       DefaultRules,
		ExtraGroups: DefaultProxyGroups(names),
	})
	for _, want := range []string{
		"PROCESS-NAME,CloudflareWARP,🚀直接连接",
		"DOMAIN-SUFFIX,deepseek.com,🇨🇳国产AI",
		"DOMAIN-SUFFIX,openai.com,🤖国外AI",
		"IP-CIDR,91.108.4.0/22,✈️Telegram,no-resolve",
		"GEOIP,CN,🚀直接连接",
		"FINAL,⚓️其他流量",
		"🚀直接连接 = select, DIRECT",
		"🔰国外流量 = select, HK, JP",
		"⚓️其他流量 = select, 🔰国外流量, 🚀直接连接",
	} {
		if !strings.Contains(withRules, want) {
			t.Fatalf("rules profile missing %q:\n%s", want, withRules)
		}
	}
	if strings.Contains(withRules, "/Users/") || strings.Contains(withRules, ".list,") {
		t.Fatalf("rule sets should be expanded inline:\n%s", withRules)
	}
	if strings.Contains(withRules, "Proxy = select") || strings.Contains(withRules, "Auto = url-test") {
		t.Fatalf("Proxy/Auto groups must not be generated:\n%s", withRules)
	}

	managed := RenderProfile(lines, names, ProfileOptions{ManagedConfigURL: "https://example.com/sub?format=profile"})
	first := strings.SplitN(managed, "\n", 2)[0]
	if !strings.HasPrefix(first, "#!MANAGED-CONFIG https://example.com/sub?format=profile ") {
		t.Fatalf("unexpected managed header: %q", first)
	}
}

func TestProxyLines(t *testing.T) {
	if got := ProxyLines(nil); got != "" {
		t.Fatalf("empty lines = %q", got)
	}
	got := ProxyLines([]string{"a=ss,1.2.3.4,8388", "b=trojan,example.com,443"})
	want := "a=ss,1.2.3.4,8388\nb=trojan,example.com,443\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSummarizeSkips(t *testing.T) {
	skips := []model.Skip{
		{Scheme: "ssr", Reason: "no SSR"},
		{Scheme: "ssr", Reason: "no SSR"},
		{Scheme: "vless", Reason: "reality over ws"},
	}
	got := SummarizeSkips(skips, 0)
	if len(got) != 2 || !strings.Contains(got[0], "ssr x2") || !strings.Contains(got[1], "vless x1") {
		t.Fatalf("unexpected summary: %v", got)
	}
	limited := SummarizeSkips(skips, 1)
	if len(limited) != 2 || !strings.Contains(limited[1], "and 1 more") {
		t.Fatalf("unexpected limited summary: %v", limited)
	}
}

func TestRuleSelection(t *testing.T) {
	groups := AvailableRuleGroups()
	if len(groups) != 12 {
		t.Fatalf("groups = %d, want 12", len(groups))
	}
	if err := ValidateSelection("all"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSelection("none"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSelection("cn-ai,bogus"); err == nil {
		t.Fatal("expected unknown group error")
	}
	if got := ParseSelection("none"); len(got) != 0 {
		t.Fatalf("none selection = %v", got)
	}
	if got := ParseSelection("all"); !got[GroupCNAI] || !got[GroupOther] {
		t.Fatalf("all selection = %v", got)
	}

	selection := Selection{GroupCNAI: true}
	rules := DefaultRulesFor(selection)
	for _, want := range []string{
		"PROCESS-NAME,CloudflareWARP,DIRECT",
		"DOMAIN-SUFFIX,deepseek.com,🇨🇳国产AI",
		"GEOIP,CN,DIRECT",
		"FINAL,DIRECT",
	} {
		if !strings.Contains(rules, want) {
			t.Fatalf("selection rules missing %q:\n%s", want, rules)
		}
	}
	if strings.Contains(rules, "DOMAIN-SUFFIX,openai.com") {
		t.Fatalf("unselected foreign AI rules should be omitted:\n%s", rules)
	}

	groupsForSelection := DefaultProxyGroupsFor([]string{"HK"}, selection)
	wantGroup := "🇨🇳国产AI = select, DIRECT, HK"
	if len(groupsForSelection) != 1 || groupsForSelection[0] != wantGroup {
		t.Fatalf("groups = %v, want [%s]", groupsForSelection, wantGroup)
	}
}

func TestGeoIPRulesComeLast(t *testing.T) {
	rules := DefaultRulesFor(AllRuleGroups())
	lines := strings.Split(rules, "\n")
	seenGeoIP := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "GEOIP,") {
			seenGeoIP = true
			continue
		}
		if seenGeoIP {
			switch {
			case strings.HasPrefix(line, "DOMAIN"),
				strings.HasPrefix(line, "DOMAIN-SUFFIX"),
				strings.HasPrefix(line, "DOMAIN-KEYWORD"),
				strings.HasPrefix(line, "IP-CIDR"),
				strings.HasPrefix(line, "URL-REGEX"),
				strings.HasPrefix(line, "USER-AGENT"):
				t.Fatalf("rule %q appears after GEOIP and may trigger DNS", line)
			}
		}
	}
	if count := strings.Count(rules, "GEOIP,CN,🚀直接连接"); count != 1 {
		t.Fatalf("GEOIP,CN count = %d, want 1", count)
	}
}
