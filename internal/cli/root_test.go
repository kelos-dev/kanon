package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kelos-dev/kanon/internal/core"
)

func TestValidateUsesHomeForRelativeAssets(t *testing.T) {
	home := t.TempDir()
	if err := core.InitHome(core.InitOptions{Home: home}); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--home", home, "validate"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("validate failed: %v\n%s", err, out.String())
	}
}

func TestRenderPrintsTargetState(t *testing.T) {
	home := t.TempDir()
	if err := core.InitHome(core.InitOptions{Home: home}); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--home", home, "render"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("render failed: %v\n%s", err, out.String())
	}

	got := out.String()
	for _, want := range []string{"==> [claude]", "CLAUDE.md", "==> [codex]", "AGENTS.md", "Shared Agent Instructions"} {
		if !strings.Contains(got, want) {
			t.Fatalf("render output missing %q\n%s", want, got)
		}
	}
}
