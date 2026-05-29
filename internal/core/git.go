package core

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

func GitStatus(home string) ([]byte, error) {
	if _, err := os.Stat(filepath.Join(home, ".git")); err != nil {
		return nil, err
	}
	cmd := exec.Command("git", "-C", home, "status", "--short")
	return cmd.CombinedOutput()
}

func RunGit(home string, stdout, stderr io.Writer, args ...string) error {
	cmdArgs := append([]string{"-C", home}, args...)
	cmd := exec.Command("git", cmdArgs...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %v: %w", args, err)
	}
	return nil
}
