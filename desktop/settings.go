package main

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
)

const settingsFilename = "settings.json"

// Appearance preference values, the same names mygo.ThemeSource uses.
const (
	themeSystem = "system"
	themeLight  = "light"
	themeDark   = "dark"
)

// Settings are the preferences that outlive a launch.
type Settings struct {
	// Theme is themeSystem, themeLight or themeDark.
	Theme string `json:"theme"`
}

func validTheme(mode string) bool {
	switch mode {
	case themeSystem, themeLight, themeDark:
		return true
	}
	return false
}

// settingsStore keeps the settings in a small JSON file next to the history.
type settingsStore struct {
	mu   sync.Mutex
	path string
	data Settings
}

func newSettingsStore(path string) *settingsStore {
	store := &settingsStore{path: path, data: Settings{Theme: themeSystem}}
	if err := store.load(); err != nil {
		log.Printf("settings: %v", err)
	}
	return store
}

func (s *settingsStore) load() error {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", s.path, err)
	}
	var data Settings
	// A file written by a newer build still loads.
	if err := json.Unmarshal(raw, &data, json.RejectUnknownMembers(false)); err != nil {
		return fmt.Errorf("parse %s: %w", s.path, err)
	}
	if validTheme(data.Theme) {
		s.data.Theme = data.Theme
	}
	return nil
}

// theme returns the stored appearance preference.
func (s *settingsStore) theme() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validTheme(s.data.Theme) {
		return themeSystem
	}
	return s.data.Theme
}

// setTheme stores the appearance preference and returns what was stored.
func (s *settingsStore) setTheme(mode string) (string, error) {
	if !validTheme(mode) {
		return s.theme(), fmt.Errorf("未知的主题模式：%q", mode)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Theme == mode {
		return mode, nil
	}
	s.data.Theme = mode
	if err := s.saveLocked(); err != nil {
		return mode, err
	}
	return mode, nil
}

// saveLocked writes the settings. Callers hold s.mu.
func (s *settingsStore) saveLocked() error {
	data, err := marshalIndented(s.data, "  ")
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	return writeFileAtomic(s.path, data, 0o600)
}
