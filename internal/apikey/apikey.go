// Package apikey resolves bmg's Gemini API key from the standard
// environment chain:
//
//  1. BMG_API_KEY        (project-specific, takes precedence)
//  2. GEMINI_API_KEY     (Google's documented name)
//  3. GOOGLE_API_KEY     (older fallback used by Vertex/google-genai SDK)
//  4. ~/.config/bmg/api_key (XDG config file, mode 0600)
//
// Reading another project's .env or shell-sourced secrets is
// deliberately out of scope — bmg never reaches across project
// boundaries to find a key. If none of the above resolve, [Resolve]
// returns an error naming all four locations so the user knows what
// to set.
package apikey

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvVars is the in-priority-order list of environment variables bmg
// consults for a Gemini API key. Exported so `bmg doctor` can surface
// resolution state to the user.
var EnvVars = []string{"BMG_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY"}

// ErrNotFound is returned by [Resolve] when no key is present in env
// or at the config-file location. Wrap-friendly for errors.Is.
var ErrNotFound = errors.New("apikey: no Gemini API key found")

// Resolve walks the env chain, then falls back to ~/.config/bmg/api_key.
// Returns the key and the source where it was found ("env:GEMINI_API_KEY",
// "config:~/.config/bmg/api_key"). The source string is intended for
// `bmg doctor` output, not log lines that might leak.
func Resolve() (key, source string, err error) {
	for _, v := range EnvVars {
		if k := strings.TrimSpace(os.Getenv(v)); k != "" {
			return k, "env:" + v, nil
		}
	}
	path, err := configPath()
	if err != nil {
		return "", "", fmt.Errorf("apikey: resolve config dir: %w", err)
	}
	// SSH-style strictness on the key file's mode. If anyone other than
	// the owner can read it (group / world bits set), refuse to use it
	// rather than silently consuming a key from a permissive file. The
	// documented contract is 0600; a wider mode usually means the user
	// copied the key from a backup and forgot to chmod.
	info, statErr := os.Stat(path)
	if statErr == nil {
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			return "", "", fmt.Errorf("apikey: %s permissions are too wide (%04o); run `chmod 600 %s` and retry",
				path, perm, path)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", fmt.Errorf("%w (checked %s and %s)",
				ErrNotFound, strings.Join(EnvVars, "/"), path)
		}
		return "", "", fmt.Errorf("apikey: read %s: %w", path, err)
	}
	if k := strings.TrimSpace(string(data)); k != "" {
		return k, "config:" + path, nil
	}
	return "", "", fmt.Errorf("%w (checked %s and %s — file exists but is empty)",
		ErrNotFound, strings.Join(EnvVars, "/"), path)
}

func configPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "bmg", "api_key"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "bmg", "api_key"), nil
}
