package core

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const remoteSourceCacheVersion = "v1"

type materializedRemoteSkill struct {
	Path          string
	ResolvedRef   string
	ContentSHA256 string
}

type materializedSkillDirectoryItem struct {
	Name string
	Path string
}

type preparedRemoteSource struct {
	ExpandedURL string
	Subdir      string
}

func materializeRemoteSkillSource(home, name string, source RemoteSource, lock *SourceLockEntry) (string, error) {
	prepared, err := prepareRemoteSource("git skill provider", name, source)
	if err != nil {
		return "", err
	}
	checkoutRef := source.Ref
	expectedHash := ""
	if lock != nil {
		checkoutRef = lock.ResolvedRef
		expectedHash = lock.ContentSHA256
	}
	cachePath := remoteSourceCachePath(home, RemoteSource{
		Type:   source.Type,
		URL:    prepared.ExpandedURL,
		Ref:    checkoutRef,
		Subdir: prepared.Subdir,
	})
	if ok, hash, err := cachedMaterializedSkillSource(name, cachePath); ok || err != nil {
		if err != nil {
			return "", err
		}
		if expectedHash != "" && hash != expectedHash {
			return "", fmt.Errorf("git skill provider %q cache hash mismatch: got %s, want %s", name, hash, expectedHash)
		}
		return cachePath, nil
	}

	result, cleanup, err := fetchRemoteSkillSource(home, name, prepared, checkoutRef)
	if err != nil {
		return "", err
	}
	defer cleanup()
	if lock != nil && result.ResolvedRef != lock.ResolvedRef {
		return "", fmt.Errorf("git skill provider %q resolved %s, want %s", name, result.ResolvedRef, lock.ResolvedRef)
	}
	if expectedHash != "" && result.ContentSHA256 != expectedHash {
		return "", fmt.Errorf("git skill provider %q hash mismatch: got %s, want %s", name, result.ContentSHA256, expectedHash)
	}
	return installMaterializedSkillSource(name, result.Path, cachePath)
}

func resolveRemoteSkillSource(home, name string, source RemoteSource) (*materializedRemoteSkill, error) {
	prepared, err := prepareRemoteSource("git skill provider", name, source)
	if err != nil {
		return nil, err
	}
	result, cleanup, err := fetchRemoteSkillSource(home, name, prepared, source.Ref)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	cachePath := remoteSourceCachePath(home, RemoteSource{
		Type:   source.Type,
		URL:    prepared.ExpandedURL,
		Ref:    result.ResolvedRef,
		Subdir: prepared.Subdir,
	})
	cachePath, err = replaceMaterializedSkillSource(name, result.Path, cachePath)
	if err != nil {
		return nil, err
	}
	result.Path = cachePath
	return result, nil
}

func prepareRemoteSource(kind, name string, source RemoteSource) (preparedRemoteSource, error) {
	if source.Type != "git" {
		return preparedRemoteSource{}, fmt.Errorf("%s %q source has unsupported type %q", kind, name, source.Type)
	}
	if strings.TrimSpace(source.Ref) == "" || strings.HasPrefix(source.Ref, "-") || strings.ContainsAny(source.Ref, "\x00\r\n") {
		return preparedRemoteSource{}, fmt.Errorf("%s %q source has invalid ref %q", kind, name, source.Ref)
	}
	expandedURL := expandEnvRefs(source.URL)
	if strings.TrimSpace(expandedURL) == "" {
		return preparedRemoteSource{}, fmt.Errorf("%s %q source requires url", kind, name)
	}
	subdir, err := cleanRemoteSubdir(source.Subdir)
	if err != nil {
		return preparedRemoteSource{}, fmt.Errorf("%s %q source subdir: %w", kind, name, err)
	}
	return preparedRemoteSource{ExpandedURL: expandedURL, Subdir: subdir}, nil
}

func fetchRemoteSkillSource(home, name string, source preparedRemoteSource, checkoutRef string) (*materializedRemoteSkill, func(), error) {
	fetched, cleanup, err := fetchRemoteSource(home, "git skill provider", name, source, checkoutRef)
	if err != nil {
		return nil, nil, err
	}
	if err := validateMaterializedSkillDirectory(name, fetched.Path); err != nil {
		cleanup()
		return nil, nil, err
	}
	contentHash, err := hashMaterializedSkillDirectory(name, fetched.Path)
	if err != nil {
		cleanup()
		return nil, nil, err
	}

	return &materializedRemoteSkill{
		Path:          fetched.Path,
		ResolvedRef:   fetched.ResolvedRef,
		ContentSHA256: contentHash,
	}, cleanup, nil
}

func fetchRemoteSource(home, kind, name string, source preparedRemoteSource, checkoutRef string) (*materializedRemoteSkill, func(), error) {
	parent := filepath.Join(home, ".kanon", "cache", "sources")
	tmpRoot, err := os.MkdirTemp(parent, ".tmp-source-*")
	if err != nil {
		if mkErr := os.MkdirAll(parent, 0o755); mkErr != nil {
			return nil, nil, fmt.Errorf("%s %q source cache: %w", kind, name, mkErr)
		}
		tmpRoot, err = os.MkdirTemp(parent, ".tmp-source-*")
		if err != nil {
			return nil, nil, fmt.Errorf("%s %q source cache: %w", kind, name, err)
		}
	}
	cleanup := func() {
		os.RemoveAll(tmpRoot)
	}

	repoPath := filepath.Join(tmpRoot, "repo")
	if err := runSourceGit(source.ExpandedURL, "", "clone", "--", source.ExpandedURL, repoPath); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("%s %q source: %w", kind, name, err)
	}
	if err := runSourceGit(source.ExpandedURL, repoPath, "checkout", "--detach", checkoutRef); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("%s %q source: %w", kind, name, err)
	}
	out, err := sourceGitOutput(source.ExpandedURL, repoPath, "rev-parse", "HEAD")
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("%s %q source: %w", kind, name, err)
	}
	resolvedRef := strings.TrimSpace(string(out))

	sourcePath := repoPath
	if source.Subdir != "" {
		sourcePath = filepath.Join(repoPath, source.Subdir)
	}
	if info, err := os.Lstat(sourcePath); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("%s %q source subdir %q: %w", kind, name, source.Subdir, err)
	} else if !info.IsDir() {
		cleanup()
		return nil, nil, fmt.Errorf("%s %q source subdir %q is not a directory", kind, name, source.Subdir)
	}
	if source.Subdir == "" {
		if err := stripGitMetadata(kind, name, sourcePath); err != nil {
			cleanup()
			return nil, nil, err
		}
	}

	return &materializedRemoteSkill{
		Path:        sourcePath,
		ResolvedRef: resolvedRef,
	}, cleanup, nil
}

func stripGitMetadata(kind, name, root string) error {
	if err := os.RemoveAll(filepath.Join(root, ".git")); err != nil {
		return fmt.Errorf("%s %q source metadata: %w", kind, name, err)
	}
	return nil
}

func cachedMaterializedSkillSource(name, cachePath string) (bool, string, error) {
	if _, err := os.Stat(cachePath); err == nil {
		if err := validateMaterializedSkillDirectory(name, cachePath); err != nil {
			return false, "", err
		}
		hash, err := hashMaterializedSkillDirectory(name, cachePath)
		if err != nil {
			return false, "", err
		}
		return true, hash, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, "", fmt.Errorf("git skill provider %q cache: %w", name, err)
	}
	return false, "", nil
}

func installMaterializedSkillSource(name, sourcePath, cachePath string) (string, error) {
	return installMaterializedSourceMode("git skill provider", name, sourcePath, cachePath, false, cachedMaterializedSkillSource)
}

func replaceMaterializedSkillSource(name, sourcePath, cachePath string) (string, error) {
	return installMaterializedSourceMode("git skill provider", name, sourcePath, cachePath, true, cachedMaterializedSkillSource)
}

func installMaterializedSourceMode(
	kind,
	name,
	sourcePath,
	cachePath string,
	replace bool,
	cached func(string, string) (bool, string, error),
) (string, error) {
	parent := filepath.Dir(cachePath)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", fmt.Errorf("%s %q source cache: %w", kind, name, err)
	}
	if replace {
		if err := os.RemoveAll(cachePath); err != nil {
			return "", fmt.Errorf("%s %q source cache: %w", kind, name, err)
		}
	}
	if err := os.Rename(sourcePath, cachePath); err != nil {
		if ok, _, cacheErr := cached(name, cachePath); ok || cacheErr != nil {
			if cacheErr != nil {
				return "", cacheErr
			}
			return cachePath, nil
		}
		return "", fmt.Errorf("%s %q source cache: %w", kind, name, err)
	}
	return cachePath, nil
}

func hashMaterializedSkill(name, root string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			return fmt.Errorf("skill %q source contains symlink %q", name, rel)
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			return fmt.Errorf("skill %q source contains non-regular file %q", name, rel)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		writeHashField(hash, []byte(filepath.ToSlash(rel)))
		writeHashField(hash, data)
		return nil
	})
	if err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func hashMaterializedSkillDirectory(name, root string) (string, error) {
	items, err := materializedSkillDirectoryItems(name, root)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	for _, item := range items {
		itemHash, err := hashMaterializedSkill(item.Name, item.Path)
		if err != nil {
			return "", err
		}
		writeHashField(hash, []byte(item.Name))
		writeHashField(hash, []byte(itemHash))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func writeHashField(hash interface{ Write([]byte) (int, error) }, data []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(data)))
	hash.Write(length[:])
	hash.Write(data)
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

func validateMaterializedSkillDirectory(name, root string) error {
	_, err := materializedSkillDirectoryItems(name, root)
	return err
}

func materializedSkillDirectoryItems(name, root string) ([]materializedSkillDirectoryItem, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("git skill provider %q: %w", name, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("git skill provider %q root is a symlink", name)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("git skill provider %q root is not a directory", name)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("git skill provider %q: %w", name, err)
	}
	var items []materializedSkillDirectoryItem
	for _, entry := range entries {
		entryName := entry.Name()
		if strings.HasPrefix(entryName, ".") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("git skill provider %q contains symlink %q", name, entryName)
		}
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entryName)
		if err := validateMaterializedSkill(entryName, path); err != nil {
			return nil, fmt.Errorf("git skill provider %q child %q: %w", name, entryName, err)
		}
		items = append(items, materializedSkillDirectoryItem{Name: entryName, Path: path})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("git skill provider %q contains no skill directories", name)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
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
	_, err := sourceGitOutput(repo, dir, args...)
	return err
}

func sourceGitOutput(repo, dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.CombinedOutput()
	if err == nil {
		return output, nil
	}
	verb := "command"
	if len(args) > 0 {
		verb = args[0]
	}
	clean := strings.TrimSpace(redactSourceGitOutput(repo, string(output)))
	if clean == "" {
		return nil, fmt.Errorf("git %s: %w", verb, err)
	}
	return nil, fmt.Errorf("git %s: %w: %s", verb, err, clean)
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
