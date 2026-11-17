package main

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	historyFilename   = "history.json"
	maxHistoryEntries = 50
	defaultLabelA     = "订阅 A"
	defaultLabelB     = "订阅 B"
)

// HistoryEntry is one previously used subscription setup. It keeps the whole
// form state so selecting an entry is enough to pull the same subscription
// again, including merges and output options.
type HistoryEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	URL2      string `json:"url2"`
	Label1    string `json:"label1"`
	Label2    string `json:"label2"`
	Merge     bool   `json:"merge"`
	UA        string `json:"ua"`
	Prefix    string `json:"prefix"`
	Filter    string `json:"filter"`
	Exclude   string `json:"exclude"`
	Sort      bool   `json:"sort"`
	UDP       bool   `json:"udp"`
	TFO       bool   `json:"tfo"`
	Insecure  bool   `json:"insecure"`
	Rules     bool   `json:"rules"`
	Groups    string `json:"groups"`
	Nodes     int    `json:"nodes"`
	Times     int    `json:"times"`
	UpdatedAt string `json:"updated_at"`
	// UpdatedNS orders the entries. RFC 3339 has only second precision, so
	// two pulls in the same second would otherwise tie and show in an
	// arbitrary order.
	UpdatedNS int64 `json:"updated_ns"`
}

// historyStore keeps the history in a JSON file in the app's user data
// directory. Subscription URLs carry credentials, so the file is private to
// the current user (0600).
type historyStore struct {
	mu      sync.Mutex
	path    string
	entries []HistoryEntry
}

func newHistoryStore(path string) *historyStore {
	store := &historyStore{path: path}
	if err := store.load(); err != nil {
		log.Printf("history: %v", err)
	}
	return store
}

func (s *historyStore) load() error {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", s.path, err)
	}
	var entries []HistoryEntry
	// RejectUnknownMembers is off so a file written by a newer build still
	// loads: a stale history is worth more than a hard failure.
	if err := json.Unmarshal(raw, &entries, json.RejectUnknownMembers(false)); err != nil {
		return fmt.Errorf("parse %s: %w", s.path, err)
	}
	s.entries = make([]HistoryEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.URL) == "" {
			continue
		}
		if entry.Times <= 0 {
			entry.Times = 1
		}
		if entry.UpdatedNS == 0 && entry.UpdatedAt != "" {
			// An entry written before updated_ns existed.
			if at, err := time.Parse(time.RFC3339, entry.UpdatedAt); err == nil {
				entry.UpdatedNS = at.UnixNano()
			}
		}
		s.entries = append(s.entries, entry)
	}
	sortHistory(s.entries)
	return nil
}

// list returns a copy ordered by most recent use first.
func (s *historyStore) list() []HistoryEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := slices.Clone(s.entries)
	sortHistory(entries)
	return entries
}

// record adds a successful pull, or updates the matching entry when the same
// subscription (or the same pair of subscriptions) was used before.
func (s *historyStore) record(req ConvertRequest, nodes int) ([]HistoryEntry, error) {
	entry := historyFromRequest(req, nodes)
	key := historyKey(entry)
	id := historyID(key)

	s.mu.Lock()
	defer s.mu.Unlock()

	entries := slices.Clone(s.entries)
	index := -1
	for i := range entries {
		if historyKey(entries[i]) == key {
			index = i
			break
		}
	}

	entry.ID = id
	now := time.Now().UTC()
	entry.UpdatedAt = now.Format(time.RFC3339)
	entry.UpdatedNS = now.UnixNano()
	if index >= 0 {
		entry.Times = max(entries[index].Times+1, 1)
		// Keep the name given when the entry was first created: an updated
		// label is often just a UI tweak for the same subscription.
		entry.Name = entries[index].Name
		entries = append(entries[:index], entries[index+1:]...)
	} else {
		entry.Times = 1
	}
	entries = append(entries, entry)
	sortHistory(entries)
	if len(entries) > maxHistoryEntries {
		entries = entries[:maxHistoryEntries]
	}

	s.entries = entries
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return slices.Clone(s.entries), nil
}

func (s *historyStore) remove(id string) ([]HistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	kept := make([]HistoryEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		if entry.ID != id {
			kept = append(kept, entry)
		}
	}
	s.entries = kept
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return slices.Clone(s.entries), nil
}

func (s *historyStore) clear() ([]HistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = nil
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return []HistoryEntry{}, nil
}

// saveLocked replaces the file atomically. Callers hold s.mu.
func (s *historyStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create history dir: %w", err)
	}
	var buf bytes.Buffer
	encoder := jsontext.NewEncoder(&buf, jsontext.WithIndent("  "))
	if err := json.MarshalEncode(encoder, s.entries); err != nil {
		return fmt.Errorf("encode history: %w", err)
	}
	data := buf.Bytes()
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".history-*.tmp")
	if err != nil {
		return fmt.Errorf("create history file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secure history file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write history: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close history: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace history: %w", err)
	}
	return nil
}

func historyFromRequest(req ConvertRequest, nodes int) HistoryEntry {
	return HistoryEntry{
		Name:     historyName(req),
		URL:      strings.TrimSpace(req.URL),
		URL2:     strings.TrimSpace(req.URL2),
		Label1:   strings.TrimSpace(req.Label1),
		Label2:   strings.TrimSpace(req.Label2),
		Merge:    req.Merge,
		UA:       strings.TrimSpace(req.UA),
		Prefix:   req.Prefix,
		Filter:   req.Filter,
		Exclude:  req.Exclude,
		Sort:     req.Sort,
		UDP:      req.UDP,
		TFO:      req.TFO,
		Insecure: req.Insecure,
		Rules:    req.Rules,
		Groups:   strings.TrimSpace(req.Groups),
		Nodes:    nodes,
	}
}

// historyKey identifies a single subscription by URL, and a merge by its
// ordered pair of URLs. Labels and output options do not create a new entry.
func historyKey(entry HistoryEntry) string {
	key := strings.TrimSpace(entry.URL)
	if entry.Merge {
		key += "\x00" + strings.TrimSpace(entry.URL2)
	}
	return key
}

func historyID(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}

func historyName(req ConvertRequest) string {
	first := sourceTitle(req.URL, req.Label1)
	if !req.Merge {
		return first
	}
	return first + " + " + sourceTitle(req.URL2, req.Label2)
}

func sourceTitle(rawURL, label string) string {
	label = strings.TrimSpace(label)
	if label != "" && label != defaultLabelA && label != defaultLabelB {
		return label
	}
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	if trimmed := strings.TrimSpace(rawURL); trimmed != "" {
		if len(trimmed) > 40 {
			trimmed = trimmed[:40] + "…"
		}
		return trimmed
	}
	return "未命名订阅"
}

// sortHistory orders entries by most recent use, then by use count so that a
// tie in time still has a meaningful order.
func sortHistory(entries []HistoryEntry) {
	slices.SortStableFunc(entries, func(a, b HistoryEntry) int {
		if a.UpdatedNS != b.UpdatedNS {
			return cmp.Compare(b.UpdatedNS, a.UpdatedNS)
		}
		return cmp.Compare(b.Times, a.Times)
	})
}
