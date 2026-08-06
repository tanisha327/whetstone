package provider

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// isolateConfig points the credential machinery at a temp directory and clears
// the environment, so a test can never read or clobber the developer's real key.
func isolateConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")
	t.Setenv(EnvKeyFile, path)
	t.Setenv(EnvAPIKey, "")
	return path
}

func TestKeyPath_HonoursOverride(t *testing.T) {
	want := isolateConfig(t)
	got, err := KeyPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("KeyPath = %q, want %q", got, want)
	}
}

func TestKeyPath_DefaultIsUnderConfigDir(t *testing.T) {
	t.Setenv(EnvKeyFile, "")
	got, err := KeyPath()
	if err != nil {
		t.Skipf("no user config dir on this platform: %v", err)
	}
	if !strings.Contains(filepath.ToSlash(got), "whetstone/credentials") {
		t.Errorf("KeyPath = %q, want it under whetstone/", got)
	}
}

func TestLoadKey_NoSources(t *testing.T) {
	isolateConfig(t)
	_, err := loadKey("")
	if !errors.Is(err, ErrNoCredential) {
		t.Errorf("err = %v, want ErrNoCredential", err)
	}
}

func TestLoadKey_Precedence(t *testing.T) {
	path := isolateConfig(t)
	if err := os.WriteFile(path, []byte("sk-from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// File only.
	if got, err := loadKey(""); err != nil || got != "sk-from-file" {
		t.Errorf("file source = %q, %v", got, err)
	}

	// Env beats file, so a shell can override for one session.
	t.Setenv(EnvAPIKey, "sk-from-env")
	if got, err := loadKey(""); err != nil || got != "sk-from-env" {
		t.Errorf("env source = %q, %v", got, err)
	}

	// Explicit beats everything.
	if got, err := loadKey("sk-explicit"); err != nil || got != "sk-explicit" {
		t.Errorf("explicit source = %q, %v", got, err)
	}
}

func TestLoadKey_EmptyFileIsAnError(t *testing.T) {
	path := isolateConfig(t)
	if err := os.WriteFile(path, []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadKey("")
	if err == nil {
		t.Fatal("expected an error for an empty key file")
	}
	if !strings.Contains(err.Error(), "-set-key") {
		t.Errorf("error should tell the user how to fix it, got: %v", err)
	}
}

func TestSaveKey_RoundTrip(t *testing.T) {
	isolateConfig(t)
	path, err := SaveKey("sk-secret-value")
	if err != nil {
		t.Fatalf("SaveKey: %v", err)
	}
	got, err := loadKey("")
	if err != nil {
		t.Fatalf("LoadKey: %v", err)
	}
	if got != "sk-secret-value" {
		t.Errorf("LoadKey = %q", got)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("key file missing: %v", statErr)
	}
}

// A credential file must not be readable by other users on a shared box.
func TestSaveKey_FileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes are not meaningful on Windows")
	}
	isolateConfig(t)
	path, err := SaveKey("sk-secret")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != fs.FileMode(0o600) {
		t.Errorf("mode = %o, want 600", perm)
	}
}

func TestSaveKey_LeavesNoTempFile(t *testing.T) {
	path := isolateConfig(t)
	if _, err := SaveKey("sk-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveKey("sk-two"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
	if got, _ := loadKey(""); got != "sk-two" {
		t.Errorf("LoadKey = %q, want the second write to have won", got)
	}
}

func TestSaveKey_Rejects(t *testing.T) {
	isolateConfig(t)
	for _, bad := range []string{"", "   ", "\n"} {
		if _, err := SaveKey(bad); err == nil {
			t.Errorf("SaveKey(%q) should be rejected", bad)
		}
	}
}

func TestDeleteKey(t *testing.T) {
	isolateConfig(t)
	if _, err := SaveKey("sk-gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := DeleteKey(); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}
	if _, err := loadKey(""); !errors.Is(err, ErrNoCredential) {
		t.Errorf("err = %v, want ErrNoCredential after delete", err)
	}
	// Deleting twice is not an error.
	if _, err := DeleteKey(); err != nil {
		t.Errorf("second DeleteKey: %v", err)
	}
}

// People paste what their notes told them to paste. Accept the common shapes
// rather than storing a key with "export OPENAI_API_KEY=" glued to the front,
// which fails later as an unexplained 401.
func TestCleanKey(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"sk-plain", "sk-plain"},
		{"  sk-padded  ", "sk-padded"},
		{"sk-newline\n", "sk-newline"},
		{"OPENAI_API_KEY=sk-prefixed", "sk-prefixed"},
		{"export OPENAI_API_KEY=sk-exported", "sk-exported"},
		{`"sk-quoted"`, "sk-quoted"},
		{"'sk-single'", "sk-single"},
		{"export OPENAI_API_KEY=\"sk-both\"", "sk-both"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := cleanKey(tc.in); got != tc.want {
			t.Errorf("cleanKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFingerprint_DoesNotRevealKey(t *testing.T) {
	// Deliberately not shaped like a real key. A fixture that looks like a live
	// "sk-proj-..." credential trips secret scanners and wastes someone's
	// afternoon proving it was never real.
	const key = "sk-EXAMPLE-not-a-real-credential-0123456789"
	got := Fingerprint(key)
	if strings.Contains(key, got) {
		t.Errorf("fingerprint %q is a literal substring of the key", got)
	}
	if strings.Contains(got, "not-a-real") {
		t.Errorf("fingerprint leaks the middle of the key: %q", got)
	}
	if !strings.HasPrefix(got, "sk-E") || !strings.HasSuffix(got, "6789") {
		t.Errorf("fingerprint = %q, want first four and last four", got)
	}
}

func TestFingerprint_ShortAndEmpty(t *testing.T) {
	if got := Fingerprint(""); got != "(none)" {
		t.Errorf("Fingerprint(\"\") = %q", got)
	}
	if got := Fingerprint("sk-short"); got != "****" {
		t.Errorf("Fingerprint(short) = %q, want a fully masked value", got)
	}
}

func TestCredentialSource(t *testing.T) {
	path := isolateConfig(t)
	if got := CredentialSource(); got != "none" {
		t.Errorf("CredentialSource = %q, want none", got)
	}
	if _, err := SaveKey("sk-file"); err != nil {
		t.Fatal(err)
	}
	if got := CredentialSource(); got != path {
		t.Errorf("CredentialSource = %q, want %q", got, path)
	}
	t.Setenv(EnvAPIKey, "sk-env")
	if got := CredentialSource(); got != "$"+EnvAPIKey {
		t.Errorf("CredentialSource = %q, want the env var to win", got)
	}
}
