package core

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateRemoteSkillSourcesCompletesMissingEntries(t *testing.T) {
	home := t.TempDir()
	repoOne := remoteSkillRepo(t, "one")
	repoTwo := remoteSkillRepo(t, "two")
	cfg := &Config{
		Version: 1,
		Skills: []Skill{
			{Name: "one", Git: &GitSkill{URL: repoOne.path, Ref: repoOne.ref}},
			{Name: "two", Git: &GitSkill{URL: repoTwo.path, Ref: repoTwo.ref}},
		},
	}

	lock, err := UpdateRemoteSkillSources(cfg, home, []string{"one"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Sources) != 2 {
		t.Fatalf("expected named update to write complete lock, got %#v", lock.Sources)
	}
	if _, ok := sourceLockEntry(lock, "skill.git.one"); !ok {
		t.Fatalf("lock missing requested skill entry: %#v", lock.Sources)
	}
	if _, ok := sourceLockEntry(lock, "skill.git.two"); !ok {
		t.Fatalf("lock missing unrequested skill entry: %#v", lock.Sources)
	}
}

func TestLockRemoteSkillSourcesIncludesSkillDirectories(t *testing.T) {
	home := t.TempDir()
	repo := remoteSkillDirectoryRepo(t)
	cfg := &Config{
		Version: 1,
		Skills: []Skill{{
			Name: "bundle",
			Git:  &GitSkill{URL: repo.path, Ref: repo.ref, Subdir: "packs"},
		}},
	}

	lock, err := LockRemoteSkillSources(cfg, home)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := sourceLockEntry(lock, "skill.git.bundle")
	if !ok {
		t.Fatalf("lock missing skill directory entry: %#v", lock.Sources)
	}
	if entry.Subdir != "packs" {
		t.Fatalf("lock did not normalize skill directory subdir: %#v", entry)
	}
	if !strings.HasPrefix(entry.ContentSHA256, "sha256:") {
		t.Fatalf("lock missing skill directory content hash: %#v", entry)
	}
}

func TestUpdateRemoteSkillSourcesCanUpdateNamedSkillDirectory(t *testing.T) {
	home := t.TempDir()
	repo := remoteSkillDirectoryRepo(t)
	cfg := &Config{
		Version: 1,
		Skills: []Skill{{
			Name: "bundle",
			Git:  &GitSkill{URL: repo.path, Ref: repo.ref, Subdir: "packs"},
		}},
	}

	lock, err := UpdateRemoteSkillSources(cfg, home, []string{"bundle"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sourceLockEntry(lock, "skill.git.bundle"); !ok {
		t.Fatalf("named update missing skill directory entry: %#v", lock.Sources)
	}
}

func TestCheckRemoteSkillSourcesReportsLegacySkillLockEntry(t *testing.T) {
	home := t.TempDir()
	if _, err := WriteSourceLock(home, &SourceLock{Sources: []SourceLockEntry{{
		Owner:         "skill.remote-review",
		Type:          "git",
		URL:           "https://example.invalid/repo.git",
		Ref:           "main",
		ResolvedRef:   "abc123",
		ContentSHA256: "sha256:abc123",
	}}}); err != nil {
		t.Fatal(err)
	}

	errs := CheckRemoteSkillSources(&Config{Version: 1}, home, false)
	joined := errorsText(errs)
	if !strings.Contains(joined, `kanon.lock has stale entry "skill.remote-review"`) {
		t.Fatalf("expected stale legacy lock entry, got: %s", joined)
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
	writeTestFile(t, filepath.Join(repo, "remote", "SKILL.md"), []byte("---\nname: remote\n---\n\n"+content+"\n"))
	ref := commitTestRepo(t, repo, "add remote skill")
	return remoteSkillFixture{path: repo, ref: ref}
}

func remoteSkillDirectoryRepo(t *testing.T) remoteSkillFixture {
	t.Helper()
	repo := t.TempDir()
	writeTestFile(t, filepath.Join(repo, "packs", "review", "SKILL.md"), []byte("---\nname: review\n---\n\nReview code.\n"))
	writeTestFile(t, filepath.Join(repo, "packs", "lint", "SKILL.md"), []byte("---\nname: lint\n---\n\nLint code.\n"))
	ref := commitTestRepo(t, repo, "add remote skill directory")
	return remoteSkillFixture{path: repo, ref: ref}
}
