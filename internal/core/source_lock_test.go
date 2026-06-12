package core

import (
	"path/filepath"
	"testing"
)

func TestUpdateRemoteSkillSourcesCompletesMissingEntries(t *testing.T) {
	home := t.TempDir()
	repoOne := remoteSkillRepo(t, "one")
	repoTwo := remoteSkillRepo(t, "two")
	cfg := &Config{
		Version: 1,
		Skills: []Skill{
			{Name: "one", Source: &RemoteSource{Type: "git", URL: repoOne.path, Ref: repoOne.ref}},
			{Name: "two", Source: &RemoteSource{Type: "git", URL: repoTwo.path, Ref: repoTwo.ref}},
		},
	}

	lock, err := UpdateRemoteSkillSources(cfg, home, []string{"one"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Sources) != 2 {
		t.Fatalf("expected named update to write complete lock, got %#v", lock.Sources)
	}
	if _, ok := sourceLockEntry(lock, "skill.one"); !ok {
		t.Fatalf("lock missing requested skill entry: %#v", lock.Sources)
	}
	if _, ok := sourceLockEntry(lock, "skill.two"); !ok {
		t.Fatalf("lock missing unrequested skill entry: %#v", lock.Sources)
	}
}

func TestRejectCredentialBearingLockURLRejectsLiteralCredentialMaterial(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{
			name: "env userinfo",
			raw:  "https://${KANON_TEST_USER}:${KANON_TEST_TOKEN}@example.com/repo.git",
		},
		{
			name:    "mixed userinfo",
			raw:     "https://${KANON_TEST_USER}:literal-secret@example.com/repo.git",
			wantErr: true,
		},
		{
			name:    "literal query value",
			raw:     "https://example.com/repo.git?token=literal-secret",
			wantErr: true,
		},
		{
			name: "env query value",
			raw:  "https://example.com/repo.git?token=${KANON_TEST_TOKEN}",
		},
		{
			name:    "env query default",
			raw:     "https://example.com/repo.git?token=${KANON_TEST_TOKEN:-literal-secret}",
			wantErr: true,
		},
		{
			name:    "literal fragment",
			raw:     "https://example.com/repo.git#literal-secret",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rejectCredentialBearingLockURL(tt.raw)
			if tt.wantErr && err == nil {
				t.Fatal("expected literal credential rejection")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected rejection: %v", err)
			}
		})
	}
}

type remoteSkillFixture struct {
	path string
	ref  string
}

func remoteSkillRepo(t *testing.T, content string) remoteSkillFixture {
	t.Helper()
	repo := t.TempDir()
	writeTestFile(t, filepath.Join(repo, "SKILL.md"), []byte("---\nname: remote\n---\n\n"+content+"\n"))
	ref := commitTestRepo(t, repo, "add remote skill")
	return remoteSkillFixture{path: repo, ref: ref}
}
