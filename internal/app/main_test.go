package app

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

var testPreferencesRoot string

func TestMain(m *testing.M) {
	hostPreferencesPath, err := preferencesPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve host preferences: %v\n", err)
		os.Exit(1)
	}
	hostPreferencesBefore, hostPreferencesExisted, err := readOptionalFile(hostPreferencesPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read host preferences before tests: %v\n", err)
		os.Exit(1)
	}

	testPreferencesRoot, err = os.MkdirTemp("", "surfsk8s-app-test-config-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create test preferences root: %v\n", err)
		os.Exit(1)
	}
	userConfigDir = func() (string, error) { return testPreferencesRoot, nil }

	code := m.Run()
	if err := os.RemoveAll(testPreferencesRoot); err != nil {
		fmt.Fprintf(os.Stderr, "remove test preferences root: %v\n", err)
		code = 1
	}

	hostPreferencesAfter, hostPreferencesStillExists, err := readOptionalFile(hostPreferencesPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read host preferences after tests: %v\n", err)
		code = 1
	} else if hostPreferencesExisted != hostPreferencesStillExists || !bytes.Equal(hostPreferencesBefore, hostPreferencesAfter) {
		fmt.Fprintf(os.Stderr, "host preferences changed during internal/app tests: %s\n", hostPreferencesPath)
		code = 1
	}
	os.Exit(code)
}

func readOptionalFile(path string) ([]byte, bool, error) {
	content, err := os.ReadFile(path)
	if err == nil {
		return content, true, nil
	}
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	return nil, false, err
}

func TestPreferencesSuiteUsesIsolatedDirectory(t *testing.T) {
	path, err := preferencesPath()
	if err != nil {
		t.Fatalf("preferencesPath: %v", err)
	}
	want := filepath.Join(testPreferencesRoot, "surfsk8s", "preferences.json")
	if path != want {
		t.Fatalf("preferences path = %q, want isolated path %q", path, want)
	}
}
