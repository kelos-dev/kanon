package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const remoteSourceCacheVersion = "v1"

func materializeRemoteSkill(home, name string, source RemoteSource) (string, error) {
	if source.Type != "git" {
		return "", fmt.Errorf("skill %q source has unsupported type %q", name, source.Type)
	}
	if strings.TrimSpace(source.Ref) == "" || strings.HasPrefix(source.Ref, "-") || strings.ContainsAny(source.Ref, "\x00\r\n") {
		return "", fmt.Errorf("skill %q source has invalid ref %q", name, source.Ref)
	}
	expandedURL := expandEnvRefs(source.URL)
	if strings.TrimSpace(expandedURL) == "" {
		return "", fmt.Errorf("skill %q source requires url", name)
	}
	subdir, err := cleanRemoteSubdir(source.Subdir)
	if err != nil {
		return "", fmt.Errorf("skill %q source subdir: %w", name, err)
	}
	cachePath := remoteSourceCachePath(home, RemoteSource{
		Type:   source.Type,
		URL:    expandedURL,
		Ref:    source.Ref,
		Subdir: subdir,
	})
	if ok, err := cachedMaterializedSkill(name, cachePath); ok || err != nil {
		if err != nil {
			return "", err
		}
		return cachePath, nil
	}

	parent := filepath.Dir(cachePath)
	tmpRoot, err := os.MkdirTemp(parent, ".tmp-source-*")
	if err != nil {
		if mkErr := os.MkdirAll(parent, 0o755); mkErr != nil {
			return "", fmt.Errorf("skill %q source cache: %w", name, mkErr)
		}
		tmpRoot, err = os.MkdirTemp(parent, ".tmp-source-*")
		if err != nil {
			return "", fmt.Errorf("skill %q source cache: %w", name, err)
		}
	}
	defer os.RemoveAll(tmpRoot)

	repoPath := filepath.Join(tmpRoot, "repo")
	if err := runSourceGit(expandedURL, "", "clone", "--", expandedURL, repoPath); err != nil {
		return "", fmt.Errorf("skill %q source: %w", name, err)
	}
	if err := runSourceGit(expandedURL, repoPath, "checkout", "--detach", source.Ref); err != nil {
		return "", fmt.Errorf("skill %q source: %w", name, err)
	}

	sourcePath := repoPath
	if subdir != "" {
		sourcePath = filepath.Join(repoPath, subdir)
	}
	if info, err := os.Lstat(sourcePath); err != nil {
		return "", fmt.Errorf("skill %q source subdir %q: %w", name, source.Subdir, err)
	} else if !info.IsDir() {
		return "", fmt.Errorf("skill %q source subdir %q is not a directory", name, source.Subdir)
	}
	if subdir == "" {
		if err := stripGitMetadata(name, sourcePath); err != nil {
			return "", err
		}
	}
	if err := validateMaterializedSkill(name, sourcePath); err != nil {
		return "", err
	}

	return installMaterializedSkill(name, sourcePath, cachePath)
}

func stripGitMetadata(name, root string) error {
	if err := os.RemoveAll(filepath.Join(root, ".git")); err != nil {
		return fmt.Errorf("skill %q source metadata: %w", name, err)
	}
	return nil
}

func cachedMaterializedSkill(name, cachePath string) (bool, error) {
	if _, err := os.Stat(filepath.Join(cachePath, "SKILL.md")); err == nil {
		if err := validateMaterializedSkill(name, cachePath); err != nil {
			return false, err
		}
		return true, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("skill %q source cache: %w", name, err)
	}
	return false, nil
}

func installMaterializedSkill(name, sourcePath, cachePath string) (string, error) {
	parent := filepath.Dir(cachePath)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("skill %q source cache: %w", name, err)
	}
	if err := os.Rename(sourcePath, cachePath); err != nil {
		if ok, cacheErr := cachedMaterializedSkill(name, cachePath); ok || cacheErr != nil {
			if cacheErr != nil {
				return "", cacheErr
			}
			return cachePath, nil
		}
		return "", fmt.Errorf("skill %q source cache: %w", name, err)
	}
	return cachePath, nil
}

func validateMaterializedSkill(name, root string) error {
	skillFile := filepath.Join(root, "SKILL.md")
	info, err := os.Lstat(skillFile)
	if err != nil {
		return fmt.Errorf("skill %q source missing SKILL.md: %w", name, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("skill %q source contains symlink %q", name, "SKILL.md")
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("skill %q source SKILL.md is not a regular file", name)
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("skill %q source root is a symlink", name)
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			return fmt.Errorf("skill %q source contains symlink %q", name, rel)
		}
		return nil
	})
}

func remoteSourceCachePath(home string, source RemoteSource) string {
	hash := sha256.Sum256([]byte(strings.Join([]string{
		remoteSourceCacheVersion,
		source.Type,
		source.URL,
		source.Ref,
		filepath.ToSlash(source.Subdir),
	}, "\x00")))
	return filepath.Join(home, ".kanon", "cache", "sources", hex.EncodeToString(hash[:])[:32])
}

func cleanRemoteSubdir(subdir string) (string, error) {
	if strings.TrimSpace(subdir) == "" {
		return "", nil
	}
	clean := filepath.Clean(filepath.FromSlash(subdir))
	if clean == "." {
		return "", nil
	}
	if filepath.IsAbs(clean) {
		return "", errors.New("must be relative")
	}
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		if part == ".." {
			return "", errors.New("must not escape the repository")
		}
	}
	return clean, nil
}

func expandEnvRefs(value string) string {
	return envRefPattern.ReplaceAllStringFunc(value, func(match string) string {
		parts := envRefPattern.FindStringSubmatch(match)
		if parts == nil {
			return match
		}
		if env := os.Getenv(parts[1]); env != "" {
			return env
		}
		if parts[2] != "" {
			return parts[3]
		}
		return ""
	})
}

func runSourceGit(repo, dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	verb := "command"
	if len(args) > 0 {
		verb = args[0]
	}
	clean := strings.TrimSpace(redactSourceGitOutput(repo, string(output)))
	if clean == "" {
		return fmt.Errorf("git %s: %w", verb, err)
	}
	return fmt.Errorf("git %s: %w: %s", verb, err, clean)
}

func redactSourceGitOutput(repo, output string) string {
	output = redactCredentials(repo, output)
	if repo != "" {
		output = strings.ReplaceAll(output, repo, "redacted")
	}
	u, err := url.Parse(repo)
	if err != nil {
		return output
	}
	if u.RawQuery != "" {
		output = strings.ReplaceAll(output, u.RawQuery, "redacted")
	}
	for _, values := range u.Query() {
		for _, value := range values {
			if value == "" {
				continue
			}
			output = strings.ReplaceAll(output, value, "redacted")
			if escaped := url.QueryEscape(value); escaped != value {
				output = strings.ReplaceAll(output, escaped, "redacted")
			}
		}
	}
	if u.Fragment != "" {
		output = strings.ReplaceAll(output, u.Fragment, "redacted")
	}
	return output
}
