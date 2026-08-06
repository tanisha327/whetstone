package provider

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The key comes from exactly three places, in this order:
//
//  1. an explicit value in code (-key-file, and tests)
//  2. $OPENAI_API_KEY
//  3. the key file written by `whetstone -set-key`
//
// Env beats the file so a shell can override for one session. There is no
// project-local source: a credential beside the source tree gets committed.
const (
	// EnvAPIKey holds the credential. Read once at startup, never written to
	// disk by the running program, and scrubbed from every error.
	EnvAPIKey = "OPENAI_API_KEY"
	// EnvBaseURL points the client at a gateway instead of OpenAI.
	EnvBaseURL = "WHETSTONE_BASE_URL"
	// EnvModel selects the model.
	EnvModel = "WHETSTONE_MODEL"
	// EnvKeyFile overrides the key file location.
	EnvKeyFile = "WHETSTONE_KEY_FILE"
)

// ErrNoCredential is returned when no API key can be found. Callers surface it
// as actionable setup instructions rather than as a transient failure.
var ErrNoCredential = errors.New("provider: no API credential configured")

// KeyPath returns the file the credential is stored in:
// <user config dir>/whetstone/credentials. Overridden by WHETSTONE_KEY_FILE.
func KeyPath() (string, error) {
	if override := strings.TrimSpace(os.Getenv(EnvKeyFile)); override != "" {
		return override, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("provider: locating config directory: %w", err)
	}
	return filepath.Join(dir, "whetstone", "credentials"), nil
}

// loadKey resolves the API key. An explicit value short-circuits the search.
// Returns ErrNoCredential when every source is empty.
func loadKey(explicit string) (string, error) {
	if k := cleanKey(explicit); k != "" {
		return k, nil
	}
	if k := cleanKey(os.Getenv(EnvAPIKey)); k != "" {
		return k, nil
	}

	path, err := KeyPath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNoCredential
	}
	if err != nil {
		return "", fmt.Errorf("provider: reading %s: %w", path, err)
	}
	if k := cleanKey(string(data)); k != "" {
		return k, nil
	}
	return "", fmt.Errorf("provider: %s is empty; run 'whetstone -set-key'", path)
}

// SaveKey writes the credential at mode 0600 and returns the path used.
//
// Atomic (temp file plus rename): a half-written key file looks exactly like a
// wrong key, and produces a baffling 401 on the next run.
func SaveKey(key string) (string, error) {
	key = cleanKey(key)
	if key == "" {
		return "", errors.New("provider: refusing to save an empty API key")
	}
	if strings.ContainsAny(key, "\n\r") {
		return "", errors.New("provider: API key contains a line break")
	}

	path, err := KeyPath()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("provider: creating %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".credentials-*.tmp")
	if err != nil {
		return "", fmt.Errorf("provider: creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	// Restrict before writing, so the secret is never briefly world-readable.
	_ = tmp.Chmod(0o600)

	if _, err := tmp.WriteString(key + "\n"); err != nil {
		tmp.Close()
		return "", fmt.Errorf("provider: writing credential: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", fmt.Errorf("provider: syncing credential: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("provider: closing credential: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", fmt.Errorf("provider: replacing %s: %w", path, err)
	}
	return path, nil
}

// DeleteKey removes the stored credential. Removing a key that is not there is
// not an error.
func DeleteKey() (string, error) {
	path, err := KeyPath()
	if err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("provider: removing %s: %w", path, err)
	}
	return path, nil
}

// CredentialSource names where the active key came from, for display by
// -check. It never returns the key itself.
func CredentialSource() string {
	if cleanKey(os.Getenv(EnvAPIKey)) != "" {
		return "$" + EnvAPIKey
	}
	path, err := KeyPath()
	if err != nil {
		return "unknown"
	}
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return "none"
}

// Fingerprint returns a non-reversible display form of a key: the first four
// and last four characters. Enough to tell two keys apart in a status line,
// useless to anyone reading over a shoulder.
func Fingerprint(key string) string {
	key = cleanKey(key)
	if key == "" {
		return "(none)"
	}
	if len(key) <= 12 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

// cleanKey trims whitespace, a trailing newline from a file, and the
// "OPENAI_API_KEY=" prefix people paste out of habit.
func cleanKey(s string) string {
	s = strings.TrimSpace(s)
	for _, prefix := range []string{EnvAPIKey + "=", "export " + EnvAPIKey + "="} {
		if rest, ok := strings.CutPrefix(s, prefix); ok {
			s = strings.TrimSpace(rest)
		}
	}
	return strings.Trim(s, `"'`)
}
