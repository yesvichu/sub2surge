package main

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
)

// marshalIndented encodes v as indented JSON. encoding/json/v2 spells this
// through an encoder rather than a MarshalIndent function.
func marshalIndented(v any, indent string) ([]byte, error) {
	var buf bytes.Buffer
	encoder := jsontext.NewEncoder(&buf, jsontext.WithIndent(indent))
	if err := json.MarshalEncode(encoder, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeFileAtomic replaces path with data, so an interrupted write cannot leave
// a truncated file behind. The file gets perm, and its directory is created.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set mode on %s: %w", tmpName, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
