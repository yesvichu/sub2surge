package pipeline

import (
	"strings"
	"testing"

	"github.com/yesvichu/sub2surge/internal/model"
)

func TestApplyFilterRenamePrefix(t *testing.T) {
	proxies := []model.Proxy{
		{Name: "HK 01", Type: "vmess", Server: "a", Port: 1},
		{Name: "US 01", Type: "trojan", Server: "b", Port: 2},
		{Name: "HK 02", Type: "ss", Server: "c", Port: 3},
	}
	got, warnings, err := Apply(proxies, Options{
		Prefix:   "P-",
		Filter:   "HK",
		Exclude:  "02",
		Rename:   []string{`HK=>Hong Kong`},
		Sort:     true,
		Insecure: true,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %+v", len(got), got)
	}
	if got[0].Name != "P-Hong Kong 01" {
		t.Fatalf("name = %q", got[0].Name)
	}
	if !got[0].Insecure {
		t.Fatal("insecure not applied")
	}
}

func TestApplyDedupeAndUniqueNames(t *testing.T) {
	proxies := []model.Proxy{
		{Name: "same", Type: "vmess", Server: "a", Port: 1, UUID: "u"},
		{Name: "same", Type: "vmess", Server: "a", Port: 1, UUID: "u"},
		{Name: "same", Type: "vmess", Server: "b", Port: 1, UUID: "u"},
	}
	got, warnings, err := Apply(proxies, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(got), got)
	}
	if got[0].Name != "same" || got[1].Name != "same 2" {
		t.Fatalf("unexpected names: %q, %q", got[0].Name, got[1].Name)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "duplicate") {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
}

func TestApplyInvalidRegex(t *testing.T) {
	if _, _, err := Apply(nil, Options{Filter: "("}); err == nil {
		t.Fatal("expected invalid filter error")
	}
	if _, _, err := Apply(nil, Options{Exclude: "["}); err == nil {
		t.Fatal("expected invalid exclude error")
	}
	if _, _, err := Apply(nil, Options{Rename: []string{"no-separator"}}); err == nil {
		t.Fatal("expected invalid rename error")
	}
}
