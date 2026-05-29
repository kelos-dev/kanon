package core

import (
	"strings"
	"testing"
)

func TestYAMLMarshalUsesTwoSpaceIndentation(t *testing.T) {
	data, err := yamlMarshal(&Config{
		Version: 1,
		Instructions: Instructions{
			Files: []string{"instructions/shared.md"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "instructions:\n  files:\n    - instructions/shared.md") {
		t.Fatalf("yaml did not use 2-space indentation:\n%s", out)
	}
	if strings.Contains(out, "instructions:\n    files:") {
		t.Fatalf("yaml used 4-space indentation:\n%s", out)
	}
}
