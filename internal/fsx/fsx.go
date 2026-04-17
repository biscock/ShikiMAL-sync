package fsx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func EnsureDir(path string) error {
	if path == "" {
		return fmt.Errorf("directory path is empty")
	}
	return os.MkdirAll(path, 0o755)
}

func WriteJSONAtomic(path string, value any) error {
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		tmp.Close()
		return fmt.Errorf("encode json: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
