package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsDefaultToSystem(t *testing.T) {
	store := newSettingsStore(filepath.Join(t.TempDir(), settingsFilename))
	if got := store.theme(); got != themeSystem {
		t.Errorf("theme = %q, want %q", got, themeSystem)
	}
}

func TestSettingsPersistTheme(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", settingsFilename)

	store := newSettingsStore(path)
	stored, err := store.setTheme(themeDark)
	if err != nil || stored != themeDark {
		t.Fatalf("setTheme = %q, %v", stored, err)
	}

	// A restart must come back up in the chosen appearance.
	reopened := newSettingsStore(path)
	if got := reopened.theme(); got != themeDark {
		t.Errorf("after reload theme = %q, want %q", got, themeDark)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", info.Mode().Perm())
	}
}

func TestSettingsRejectUnknownTheme(t *testing.T) {
	store := newSettingsStore(filepath.Join(t.TempDir(), settingsFilename))
	if _, err := store.setTheme(themeDark); err != nil {
		t.Fatal(err)
	}

	if _, err := store.setTheme("solarized"); err == nil {
		t.Fatal("an unknown theme should be rejected")
	}
	// The rejection must not have changed what was stored.
	if got := store.theme(); got != themeDark {
		t.Errorf("theme = %q after rejecting, want %q", got, themeDark)
	}
}

func TestSettingsIgnoreCorruptAndUnknownFields(t *testing.T) {
	dir := t.TempDir()

	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := newSettingsStore(corrupt).theme(); got != themeSystem {
		t.Errorf("corrupt file gave theme %q, want %q", got, themeSystem)
	}

	// A file written by a newer build keeps loading.
	newer := filepath.Join(dir, "newer.json")
	if err := os.WriteFile(newer, []byte(`{"theme":"light","future":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := newSettingsStore(newer).theme(); got != themeLight {
		t.Errorf("unknown field gave theme %q, want %q", got, themeLight)
	}

	// A value this build does not know falls back instead of wedging the app.
	unknown := filepath.Join(dir, "unknown.json")
	if err := os.WriteFile(unknown, []byte(`{"theme":"solarized"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := newSettingsStore(unknown).theme(); got != themeSystem {
		t.Errorf("unknown theme gave %q, want %q", got, themeSystem)
	}
}

// TestSetThemeAppliesTheChoice checks the wiring main relies on: the stored
// preference is what becomes the app's appearance.
func TestSetThemeAppliesTheChoice(t *testing.T) {
	var applied []string
	original := applyTheme
	applyTheme = func(mode string) { applied = append(applied, mode) }
	t.Cleanup(func() { applyTheme = original })

	dir := t.TempDir()
	c := newConverter(dir)

	if _, err := c.SetTheme(themeLight); err != nil {
		t.Fatalf("SetTheme: %v", err)
	}
	if _, err := c.SetTheme("solarized"); err == nil {
		t.Fatal("an unknown theme should fail")
	}

	if len(applied) != 1 || applied[0] != themeLight {
		t.Errorf("applied %v, want [%s]", applied, themeLight)
	}
	// A fresh process must come back in the saved appearance.
	if got := c.Theme(); got != themeLight {
		t.Errorf("theme = %q, want %q", got, themeLight)
	}
	if got := newConverter(dir).Theme(); got != themeLight {
		t.Errorf("after restart theme = %q, want %q", got, themeLight)
	}
}

func TestWriteFileAtomicReplacesContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("new"), 0o600); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Errorf("content = %q, want %q", data, "new")
	}

	// No temporary files may be left behind next to it.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("left %d entries behind: %v", len(entries), entries)
	}
}
