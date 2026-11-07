package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ConfigDir is HANDLE_CONFIG_DIR, or ~/.config/handle-mcp on every OS. It is read at each call,
// so a test can change it. A leading "~" is the home folder, as in Python.
func ConfigDir() (string, error) {
	dir := os.Getenv("HANDLE_CONFIG_DIR")
	if dir == "" {
		dir = "~/.config/handle-mcp"
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", Errorf("Kan ikke finde din hjemmemappe. Sæt HANDLE_CONFIG_DIR.")
		}
		dir = filepath.Join(home, dir[1:])
	}
	return dir, nil
}

// ConfigSet saves a session in <config dir>/<store>.json, readable by the owner only. It is
// used by `handle-mcp login <store> --from-env`, where there is no keychain. An existing wider
// file is tightened before anything is written. Returns the path.
func ConfigSet(store, email string, session map[string]string) (string, error) {
	path, err := configPath(store)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(saved{Email: email, Session: session})
	if err != nil {
		return "", Errorf("Kunne ikke gemme i %s.", path)
	}
	if err := writePrivate(path, data); err != nil {
		return "", Errorf("Kunne ikke gemme i %s (%s).", path, reason(err))
	}
	return path, nil
}

func writePrivate(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if runtime.GOOS != "windows" { // Windows ignores modes (ADR 0004)
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := f.Chmod(0o600); err != nil { // tighten before writing
			f.Close()
			return err
		}
	}
	if err := f.Truncate(0); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// reason is the operating system's words (e.g. "permission denied") without the path.
func reason(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return "ukendt fejl"
}

// ConfigDelete is true if <config dir>/<store>.json was removed.
func ConfigDelete(store string) (bool, error) {
	path, err := configPath(store)
	if err != nil {
		return false, err
	}
	err = os.Remove(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	}
	return false, Errorf("Kunne ikke slette %s (%s).", path, reason(err))
}
