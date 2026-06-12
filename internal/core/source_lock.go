package core

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const sourceLockVersion = 1

var gitFullSHAPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}([0-9a-fA-F]{24})?$`)

type remoteSkillSource struct {
	Name   string
	Owner  string
	Source RemoteSource
}

func SourceLockPath(home string) string {
	return filepath.Join(home, "kanon.lock")
}

func LoadSourceLock(home string) (*SourceLock, string, error) {
	path := SourceLockPath(home)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, path, nil
	}
	if err != nil {
		return nil, path, err
	}
	var lock SourceLock
	if err := yaml.Unmarshal(data, &lock); err != nil {
		return nil, path, err
	}
	if err := validateSourceLock(&lock); err != nil {
		return nil, path, err
	}
	sortSourceLock(&lock)
	return &lock, path, nil
}

func WriteSourceLock(home string, lock *SourceLock) (string, error) {
	if lock == nil {
		lock = &SourceLock{}
	}
	lock.Version = sourceLockVersion
	sortSourceLock(lock)
	if err := validateSourceLock(lock); err != nil {
		return "", err
	}
	path := SourceLockPath(home)
	data, err := yamlMarshal(lock)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o644)
}

func LockRemoteSkillSources(cfg *Config, home string) (*SourceLock, error) {
	lock, _, err := LoadSourceLock(home)
	if err != nil {
		return nil, err
	}
	existing := sourceLockEntriesByOwner(lock)
	items := enabledRemoteSkillSources(cfg)
	entries := make([]SourceLockEntry, 0, len(items))
	for _, item := range items {
		if entry, ok := existing[item.Owner]; ok {
			if err := entryMatchesRemoteSource(entry, item.Source); err == nil {
				if err := rejectCredentialBearingLockURL(item.Source.URL); err != nil {
					return nil, fmt.Errorf("skill %q source url: %w", item.Name, err)
				}
				entries = append(entries, entry)
				continue
			}
		}
		entry, err := resolveSourceLockEntry(home, item)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	next := &SourceLock{Version: sourceLockVersion, Sources: entries}
	sortSourceLock(next)
	if err := validateSourceLock(next); err != nil {
		return nil, err
	}
	return next, nil
}

func resolveRemoteSkillSources(cfg *Config, home string) (*SourceLock, error) {
	items := enabledRemoteSkillSources(cfg)
	entries := make([]SourceLockEntry, 0, len(items))
	for _, item := range items {
		entry, err := resolveSourceLockEntry(home, item)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	lock := &SourceLock{Version: sourceLockVersion, Sources: entries}
	sortSourceLock(lock)
	if err := validateSourceLock(lock); err != nil {
		return nil, err
	}
	return lock, nil
}

func UpdateRemoteSkillSources(cfg *Config, home string, names []string, all bool) (*SourceLock, error) {
	if all {
		return resolveRemoteSkillSources(cfg, home)
	}
	if len(names) == 0 {
		return nil, errors.New("lock update requires a skill name or --all")
	}
	items := enabledRemoteSkillSources(cfg)
	currentByName := map[string]remoteSkillSource{}
	updateNames := map[string]bool{}
	for _, item := range items {
		currentByName[item.Name] = item
	}
	for _, name := range names {
		if _, ok := currentByName[name]; !ok {
			return nil, fmt.Errorf("enabled remote skill %q not found", name)
		}
		updateNames[name] = true
	}

	lock, _, err := LoadSourceLock(home)
	if err != nil {
		return nil, err
	}
	existing := sourceLockEntriesByOwner(lock)
	entries := map[string]SourceLockEntry{}
	for _, item := range items {
		if updateNames[item.Name] {
			continue
		}
		if entry, ok := existing[item.Owner]; ok {
			if err := entryMatchesRemoteSource(entry, item.Source); err == nil {
				if err := rejectCredentialBearingLockURL(item.Source.URL); err != nil {
					return nil, fmt.Errorf("skill %q source url: %w", item.Name, err)
				}
				entries[item.Owner] = entry
				continue
			}
		}
		entry, err := resolveSourceLockEntry(home, item)
		if err != nil {
			return nil, err
		}
		entries[item.Owner] = entry
	}
	for _, name := range names {
		item := currentByName[name]
		entry, err := resolveSourceLockEntry(home, item)
		if err != nil {
			return nil, err
		}
		entries[item.Owner] = entry
	}
	next := &SourceLock{Version: sourceLockVersion, Sources: make([]SourceLockEntry, 0, len(entries))}
	for _, entry := range entries {
		next.Sources = append(next.Sources, entry)
	}
	sortSourceLock(next)
	if err := validateSourceLock(next); err != nil {
		return nil, err
	}
	return next, nil
}

func CheckRemoteSkillSources(cfg *Config, home string, requireLocked bool) []error {
	var errs []error
	items := enabledRemoteSkillSources(cfg)
	lock, path, err := LoadSourceLock(home)
	if err != nil {
		return []error{err}
	}
	if lock == nil {
		if requireLocked && len(items) > 0 {
			return []error{fmt.Errorf("%s is required for remote sources; run kanon lock", path)}
		}
		return nil
	}

	current := map[string]remoteSkillSource{}
	for _, item := range items {
		current[item.Owner] = item
	}
	for _, entry := range lock.Sources {
		if strings.HasPrefix(entry.Owner, "skill.") {
			if _, ok := current[entry.Owner]; !ok {
				errs = append(errs, fmt.Errorf("kanon.lock has stale entry %q", entry.Owner))
			}
		}
	}

	for _, item := range items {
		entry, ok := sourceLockEntry(lock, item.Owner)
		if !ok {
			if requireLocked {
				errs = append(errs, fmt.Errorf("remote skill %q is missing from kanon.lock", item.Name))
			}
			continue
		}
		if err := entryMatchesRemoteSource(entry, item.Source); err != nil {
			errs = append(errs, err)
			continue
		}
		lockedEntry := entry
		if _, err := materializeRemoteSkill(home, item.Name, item.Source, &lockedEntry); err != nil {
			errs = append(errs, err)
			continue
		}
		resolved, err := resolveRemoteSkillSource(home, item.Name, item.Source)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if resolved.ResolvedRef != entry.ResolvedRef {
			errs = append(errs, fmt.Errorf("remote skill %q resolves to %s, but kanon.lock pins %s", item.Name, resolved.ResolvedRef, entry.ResolvedRef))
		}
		if resolved.ContentSHA256 != entry.ContentSHA256 {
			errs = append(errs, fmt.Errorf("remote skill %q content hash is %s, but kanon.lock pins %s", item.Name, resolved.ContentSHA256, entry.ContentSHA256))
		}
	}
	return errs
}

func SourceLockWarnings(cfg *Config, lock *SourceLock) []string {
	var warnings []string
	for _, item := range enabledRemoteSkillSources(cfg) {
		if isImmutableGitRef(item.Source.Ref) {
			continue
		}
		entry, ok := sourceLockEntry(lock, item.Owner)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("skill %q source uses mutable ref %q without a matching kanon.lock entry", item.Name, item.Source.Ref))
			continue
		}
		if err := entryMatchesRemoteSource(entry, item.Source); err != nil {
			warnings = append(warnings, fmt.Sprintf("skill %q source uses mutable ref %q without a matching kanon.lock entry", item.Name, item.Source.Ref))
		}
	}
	return warnings
}

func sourceLockEntry(lock *SourceLock, owner string) (SourceLockEntry, bool) {
	if lock == nil {
		return SourceLockEntry{}, false
	}
	for _, entry := range lock.Sources {
		if entry.Owner == owner {
			return entry, true
		}
	}
	return SourceLockEntry{}, false
}

func sourceLockEntriesByOwner(lock *SourceLock) map[string]SourceLockEntry {
	entries := map[string]SourceLockEntry{}
	if lock == nil {
		return entries
	}
	for _, entry := range lock.Sources {
		entries[entry.Owner] = entry
	}
	return entries
}

func entryMatchesRemoteSource(entry SourceLockEntry, source RemoteSource) error {
	normalized, err := normalizeRemoteSource(source)
	if err != nil {
		return err
	}
	if entry.Type != normalized.Type {
		return fmt.Errorf("kanon.lock entry %q type is %q, but kanon.yaml has %q; run kanon lock", entry.Owner, entry.Type, normalized.Type)
	}
	if entry.URL != normalized.URL {
		return fmt.Errorf("kanon.lock entry %q url is stale; run kanon lock", entry.Owner)
	}
	if entry.Ref != normalized.Ref {
		return fmt.Errorf("kanon.lock entry %q ref is %q, but kanon.yaml has %q; run kanon lock", entry.Owner, entry.Ref, normalized.Ref)
	}
	if entry.Subdir != normalized.Subdir {
		return fmt.Errorf("kanon.lock entry %q subdir is %q, but kanon.yaml has %q; run kanon lock", entry.Owner, entry.Subdir, normalized.Subdir)
	}
	return nil
}

func resolveSourceLockEntry(home string, item remoteSkillSource) (SourceLockEntry, error) {
	if err := rejectCredentialBearingLockURL(item.Source.URL); err != nil {
		return SourceLockEntry{}, fmt.Errorf("skill %q source url: %w", item.Name, err)
	}
	normalized, err := normalizeRemoteSource(item.Source)
	if err != nil {
		return SourceLockEntry{}, err
	}
	resolved, err := resolveRemoteSkillSource(home, item.Name, item.Source)
	if err != nil {
		return SourceLockEntry{}, err
	}
	return SourceLockEntry{
		Owner:         item.Owner,
		Type:          normalized.Type,
		URL:           normalized.URL,
		Ref:           normalized.Ref,
		Subdir:        normalized.Subdir,
		ResolvedRef:   resolved.ResolvedRef,
		ContentSHA256: resolved.ContentSHA256,
	}, nil
}

func normalizeRemoteSource(source RemoteSource) (RemoteSource, error) {
	subdir, err := cleanRemoteSubdir(source.Subdir)
	if err != nil {
		return RemoteSource{}, err
	}
	return RemoteSource{
		Type:   source.Type,
		URL:    source.URL,
		Ref:    source.Ref,
		Subdir: filepath.ToSlash(subdir),
	}, nil
}

func enabledRemoteSkillSources(cfg *Config) []remoteSkillSource {
	var items []remoteSkillSource
	for _, skill := range cfg.Skills {
		if !enabled(skill.Enabled) || skill.Source == nil {
			continue
		}
		items = append(items, remoteSkillSource{
			Name:   skill.Name,
			Owner:  remoteSkillSourceOwner(skill.Name),
			Source: *skill.Source,
		})
	}
	return items
}

func remoteSkillSourceOwner(name string) string {
	return "skill." + name
}

func rejectCredentialBearingLockURL(raw string) error {
	if userinfo, ok := rawURLUserinfo(raw); ok {
		for _, component := range strings.SplitN(userinfo, ":", 2) {
			if decoded, err := url.PathUnescape(component); err == nil {
				component = decoded
			}
			if component != "" && !containsOnlyRequiredEnvRefs(component) {
				return errors.New("contains literal credentials; use environment references instead")
			}
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	if parsed.User != nil {
		if username := parsed.User.Username(); username != "" && !containsOnlyRequiredEnvRefs(username) {
			return errors.New("contains literal credentials; use environment references instead")
		}
		if password, ok := parsed.User.Password(); ok && password != "" && !containsOnlyRequiredEnvRefs(password) {
			return errors.New("contains literal credentials; use environment references instead")
		}
	}
	for _, values := range parsed.Query() {
		for _, value := range values {
			if value != "" && !containsOnlyRequiredEnvRefs(value) {
				return errors.New("contains literal credentials; use environment references instead")
			}
		}
	}
	if parsed.Fragment != "" && !containsOnlyRequiredEnvRefs(parsed.Fragment) {
		return errors.New("contains literal credentials; use environment references instead")
	}
	return nil
}

func rawURLUserinfo(raw string) (string, bool) {
	rest := raw
	if scheme := strings.Index(rest, "://"); scheme >= 0 {
		rest = rest[scheme+len("://"):]
	} else if strings.HasPrefix(rest, "//") {
		rest = strings.TrimPrefix(rest, "//")
	} else {
		return "", false
	}
	if end := strings.IndexAny(rest, "/?#"); end >= 0 {
		rest = rest[:end]
	}
	at := strings.LastIndex(rest, "@")
	if at < 0 {
		return "", false
	}
	return rest[:at], true
}

func containsOnlyRequiredEnvRefs(value string) bool {
	offset := 0
	found := false
	for _, loc := range envRefPattern.FindAllStringIndex(value, -1) {
		if loc[0] != offset {
			return false
		}
		parts := envRefPattern.FindStringSubmatch(value[loc[0]:loc[1]])
		if parts == nil || parts[2] != "" {
			return false
		}
		found = true
		offset = loc[1]
	}
	return found && offset == len(value)
}

func isImmutableGitRef(ref string) bool {
	return gitFullSHAPattern.MatchString(ref)
}

func validateSourceLock(lock *SourceLock) error {
	if lock.Version != sourceLockVersion {
		return fmt.Errorf("unsupported kanon.lock version %d", lock.Version)
	}
	seen := map[string]bool{}
	var errs []error
	for _, entry := range lock.Sources {
		if strings.TrimSpace(entry.Owner) == "" {
			errs = append(errs, errors.New("kanon.lock source owner cannot be empty"))
		}
		if seen[entry.Owner] {
			errs = append(errs, fmt.Errorf("kanon.lock has duplicate source owner %q", entry.Owner))
		}
		seen[entry.Owner] = true
		if entry.Type != "git" {
			errs = append(errs, fmt.Errorf("kanon.lock source %q has unsupported type %q", entry.Owner, entry.Type))
		}
		if strings.TrimSpace(entry.URL) == "" {
			errs = append(errs, fmt.Errorf("kanon.lock source %q requires url", entry.Owner))
		}
		if strings.TrimSpace(entry.Ref) == "" {
			errs = append(errs, fmt.Errorf("kanon.lock source %q requires ref", entry.Owner))
		}
		if strings.TrimSpace(entry.ResolvedRef) == "" {
			errs = append(errs, fmt.Errorf("kanon.lock source %q requires resolved_ref", entry.Owner))
		}
		if !strings.HasPrefix(entry.ContentSHA256, "sha256:") {
			errs = append(errs, fmt.Errorf("kanon.lock source %q requires content_sha256", entry.Owner))
		}
		if _, err := cleanRemoteSubdir(entry.Subdir); err != nil {
			errs = append(errs, fmt.Errorf("kanon.lock source %q has invalid subdir %q: %w", entry.Owner, entry.Subdir, err))
		}
	}
	return errors.Join(errs...)
}

func sortSourceLock(lock *SourceLock) {
	sort.Slice(lock.Sources, func(i, j int) bool {
		if lock.Sources[i].Owner == lock.Sources[j].Owner {
			return lock.Sources[i].URL < lock.Sources[j].URL
		}
		return lock.Sources[i].Owner < lock.Sources[j].Owner
	})
}
