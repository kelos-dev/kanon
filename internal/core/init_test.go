package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloneHomeClonesRepo(t *testing.T) {
	src := t.TempDir()
	runGit(t, src, "init")
	runGit(t, src, "config", "user.email", "test@example.com")
	runGit(t, src, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(src, "kanon.yaml"), []byte(sampleConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, src, "add", ".")
	runGit(t, src, "commit", "-m", "initial")

	home := filepath.Join(t.TempDir(), "home")
	if err := CloneHome(src, home); err != nil {
		t.Fatalf("clone failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "kanon.yaml")); err != nil {
		t.Fatalf("cloned home missing kanon.yaml: %v", err)
	}
}

func TestCloneHomeRejectsNonEmptyHome(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "existing"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := CloneHome("https://example.com/repo.git", home)
	if err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("expected non-empty home error, got %v", err)
	}
}

func TestCloneHomeRedactsCredentialsInError(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	// example.invalid never resolves, so git fails fast without network.
	err := CloneHome("https://carol:s3cr3t-token@example.invalid/repo.git", home)
	if err == nil {
		t.Fatal("expected clone of an unresolvable host to fail")
	}
	msg := err.Error()
	if strings.Contains(msg, "s3cr3t-token") || strings.Contains(msg, "carol:") {
		t.Fatalf("clone error leaked credentials: %s", msg)
	}
	if !strings.Contains(msg, "example.invalid") {
		t.Fatalf("clone error should still name the host: %s", msg)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}
