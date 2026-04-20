package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

var userConfigDir = os.UserConfigDir

type preferences struct {
	SelectedContexts []string `json:"selected_contexts"`
}

func preferencesPath() (string, error) {
	dir, err := userConfigDir()
	if err != nil {
		return "", fmt.Errorf("find config dir: %w", err)
	}
	return filepath.Join(dir, "surfsk8s", "preferences.json"), nil
}

func loadPreferences() (preferences, error) {
	path, err := preferencesPath()
	if err != nil {
		return preferences{}, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return preferences{}, nil
		}
		return preferences{}, fmt.Errorf("read preferences: %w", err)
	}
	var prefs preferences
	if err := json.Unmarshal(content, &prefs); err != nil {
		return preferences{}, fmt.Errorf("decode preferences: %w", err)
	}
	return prefs, nil
}

func savePreferences(prefs preferences) error {
	path, err := preferencesPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir preferences dir: %w", err)
	}
	sort.Strings(prefs.SelectedContexts)
	content, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return fmt.Errorf("encode preferences: %w", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("write preferences: %w", err)
	}
	return nil
}
