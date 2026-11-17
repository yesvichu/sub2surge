package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// subBody returns a real V2Ray subscription payload: base64 of newline
// separated proxy URIs, which is what an upstream actually serves.
func subBody(host string, names ...string) string {
	var lines []string
	for i, name := range names {
		// Distinct ports keep the pipeline's duplicate removal from merging
		// the nodes: it keys on server, port and credentials.
		lines = append(lines, fmt.Sprintf("trojan://pass%d@%s:%d?sni=%s#%s", i, host, 443+i, host, name))
	}
	return base64.StdEncoding.EncodeToString([]byte(strings.Join(lines, "\n")))
}

// newUpstream serves a real subscription over local HTTP, so Convert exercises
// the fetch, parse, convert and history path end to end.
func newUpstream(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestConvertRecordsHistoryEndToEnd is the runtime check the unit tests of the
// store cannot give: a real pull through Converter must appear in History, and
// a merge must be recorded as its own entry.
func TestConvertRecordsHistoryEndToEnd(t *testing.T) {
	dir := t.TempDir()
	c := newConverter(dir)
	ctx := t.Context()

	first := newUpstream(t, subBody("a.example.com", "HK-1", "HK-2"))
	second := newUpstream(t, subBody("b.example.com", "US-1"))

	single, err := c.Convert(ctx, ConvertRequest{
		URL: first.URL, Label1: "订阅 A", Sort: true, UDP: true, Rules: true, Groups: "all",
	})
	if err != nil {
		t.Fatalf("single Convert: %v", err)
	}
	if single.Converted != 2 {
		t.Fatalf("single converted = %d, want 2 (skips: %+v)", single.Converted, single.Skipped)
	}

	entries := c.History()
	if len(entries) != 1 {
		t.Fatalf("after one pull History has %d entries: %+v", len(entries), entries)
	}
	got := entries[0]
	if got.URL != first.URL || got.Merge {
		t.Errorf("entry does not describe the single pull: %+v", got)
	}
	if got.Nodes != 2 || got.Times != 1 {
		t.Errorf("entry nodes/times = %d/%d, want 2/1", got.Nodes, got.Times)
	}
	if !got.UDP || !got.Sort || !got.Rules || got.Groups != "all" {
		t.Errorf("entry lost the output options: %+v", got)
	}
	if got.Label1 != "订阅 A" {
		t.Errorf("entry label = %q", got.Label1)
	}

	merged, err := c.Convert(ctx, ConvertRequest{
		URL: first.URL, URL2: second.URL,
		Label1: "线路 A", Label2: "线路 B",
		Merge: true, Rules: true, Groups: "none",
	})
	if err != nil {
		t.Fatalf("merge Convert: %v", err)
	}
	if merged.Sources != 2 || merged.Converted != 3 {
		t.Fatalf("merge sources/converted = %d/%d, want 2/3", merged.Sources, merged.Converted)
	}

	entries = c.History()
	if len(entries) != 2 {
		t.Fatalf("after merge History has %d entries: %+v", len(entries), entries)
	}
	if !entries[0].Merge || entries[0].URL2 != second.URL || entries[0].Groups != "none" {
		t.Errorf("merge entry is not first or lost its state: %+v", entries[0])
	}
	if entries[0].Nodes != 3 {
		t.Errorf("merge entry nodes = %d, want 3", entries[0].Nodes)
	}

	// Re-pulling an existing subscription updates it instead of duplicating.
	again, err := c.Convert(ctx, ConvertRequest{URL: first.URL, Rules: true, Groups: "all"})
	if err != nil {
		t.Fatalf("re-pull: %v", err)
	}
	if again.Converted != 2 {
		t.Fatalf("re-pull converted = %d", again.Converted)
	}
	entries = c.History()
	if len(entries) != 2 {
		t.Fatalf("re-pull created a duplicate: %+v", entries)
	}
	if entries[0].Times != 2 {
		t.Errorf("re-pull times = %d, want 2", entries[0].Times)
	}

	// The history survives an app restart, which is what makes it useful.
	reopened := newConverter(dir)
	if got := reopened.History(); len(got) != 2 {
		t.Fatalf("history lost on restart: %+v", got)
	}

	// Selecting a history entry and pulling again is the update flow; pick the
	// merge entry the way a click on that row would.
	saved := findEntry(t, reopened.History(), func(e HistoryEntry) bool { return e.Merge })
	refreshed, err := reopened.Convert(ctx, ConvertRequest{
		URL: saved.URL, URL2: saved.URL2, Label1: saved.Label1, Label2: saved.Label2,
		Merge: saved.Merge, UA: saved.UA, Prefix: saved.Prefix, Filter: saved.Filter,
		Exclude: saved.Exclude, Sort: saved.Sort, UDP: saved.UDP, TFO: saved.TFO,
		Insecure: saved.Insecure, Rules: saved.Rules, Groups: saved.Groups,
	})
	if err != nil {
		t.Fatalf("refresh from history: %v", err)
	}
	if refreshed.Converted != 3 || refreshed.Sources != 2 {
		t.Errorf("refresh produced %d nodes from %d sources, want 3/2",
			refreshed.Converted, refreshed.Sources)
	}

	if _, err := reopened.ForgetHistory(saved.ID); err != nil {
		t.Fatalf("ForgetHistory: %v", err)
	}
	if got := reopened.History(); len(got) != 1 {
		t.Errorf("ForgetHistory left %d entries", len(got))
	}
	if got, err := reopened.ClearHistory(); err != nil || len(got) != 0 {
		t.Errorf("ClearHistory = %+v, %v", got, err)
	}
}

// findEntry returns the first history entry matching want.
func findEntry(t *testing.T, entries []HistoryEntry, want func(HistoryEntry) bool) HistoryEntry {
	t.Helper()
	for _, entry := range entries {
		if want(entry) {
			return entry
		}
	}
	t.Fatalf("no matching history entry in %+v", entries)
	return HistoryEntry{}
}

// TestConvertFailureIsNotRecorded keeps failed pulls, such as a wrong token,
// out of the history so it holds only what actually worked.
func TestConvertFailureIsNotRecorded(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer failing.Close()

	c := newConverter(t.TempDir())
	if _, err := c.Convert(t.Context(), ConvertRequest{URL: failing.URL}); err == nil {
		t.Fatal("Convert succeeded against a 403 upstream")
	}
	if got := c.History(); len(got) != 0 {
		t.Errorf("failed pull was recorded: %+v", got)
	}
}
