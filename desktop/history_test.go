package main

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHistoryRecordUpdatesAndSorts(t *testing.T) {
	store := newHistoryStore(filepath.Join(t.TempDir(), "history.json"))

	first := ConvertRequest{URL: "https://a.example/sub?token=one", Label1: "A", Rules: true, Groups: "all"}
	second := ConvertRequest{
		URL: "https://a.example/sub?token=one", URL2: "https://b.example/sub?token=two",
		Label1: "A", Label2: "B", Merge: true, Rules: true, Groups: "all",
	}

	if _, err := store.record(first, 10); err != nil {
		t.Fatalf("record first: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, err := store.record(second, 20); err != nil {
		t.Fatalf("record merge: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	updated := ConvertRequest{URL: first.URL, Label1: "renamed", Rules: true, Groups: "all"}
	if _, err := store.record(updated, 12); err != nil {
		t.Fatalf("record update: %v", err)
	}

	entries := store.list()
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d: %+v", len(entries), entries)
	}
	// The single subscription was recorded again last, so it is the most
	// recent; the merge came before it.
	if entries[0].Merge || entries[0].Times != 2 || entries[0].Nodes != 12 {
		t.Errorf("most recent entry should be the re-pulled single with 2 pulls and 12 nodes, got %+v", entries[0])
	}
	if !entries[1].Merge || entries[1].URL2 != second.URL2 {
		t.Errorf("second entry should be the merge, got %+v", entries[1])
	}
	if entries[0].Name == "renamed" {
		t.Error("entry name should come from the first record, not the latest label")
	}

	// Reload from disk: entries and usage counts must survive a restart.
	reloaded := newHistoryStore(store.path)
	got := reloaded.list()
	if len(got) != 2 || got[0].ID != entries[0].ID || got[0].Times != 2 {
		t.Errorf("history did not survive reload: %+v", got)
	}
}

func TestHistoryRemoveAndClear(t *testing.T) {
	dir := t.TempDir()
	store := newHistoryStore(filepath.Join(dir, "history.json"))
	for _, req := range []ConvertRequest{
		{URL: "https://a.example/sub"},
		{URL: "https://b.example/sub"},
	} {
		if _, err := store.record(req, 1); err != nil {
			t.Fatal(err)
		}
	}

	entries := store.list()
	if _, err := store.remove(entries[0].ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got := store.list(); len(got) != 1 || got[0].ID == entries[0].ID {
		t.Errorf("remove left unexpected entries: %+v", got)
	}

	if _, err := store.clear(); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := store.list(); len(got) != 0 {
		t.Errorf("clear left %d entries", len(got))
	}
	raw, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	var saved []HistoryEntry
	if err := json.Unmarshal(raw, &saved); err != nil || len(saved) != 0 {
		t.Errorf("cleared history file: %s (%v)", raw, err)
	}
	info, err := os.Stat(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("history file permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestHistoryKeyAndName(t *testing.T) {
	entry := historyFromRequest(ConvertRequest{
		URL: "https://sub.example/one", URL2: "https://sub.example/two",
		Label1: "订阅 A", Label2: "My line", Merge: true,
	}, 5)
	if entry.Name != "sub.example + My line" {
		t.Errorf("name = %q", entry.Name)
	}
	if historyKey(entry) != "https://sub.example/one\x00https://sub.example/two" {
		t.Errorf("merge key = %q", historyKey(entry))
	}

	single := historyFromRequest(ConvertRequest{URL: "https://sub.example/one"}, 3)
	if historyKey(single) != "https://sub.example/one" {
		t.Errorf("single key = %q", historyKey(single))
	}
	if single.Name != "sub.example" {
		t.Errorf("single name = %q", single.Name)
	}
	if historyID(single.URL) == "" {
		t.Error("empty id")
	}
}

func TestHistoryInvalidFileIsIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := newHistoryStore(path)
	if got := store.list(); len(got) != 0 {
		t.Errorf("got %d entries from broken file", len(got))
	}
}
