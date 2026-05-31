# kanon

Manage multiple coding-agent settings across multiple diverse machines.

## Quick start

```sh
kanon init --home ~/.config/kanon
kanon validate --home ~/.config/kanon
kanon diff --home ~/.config/kanon
kanon apply --home ~/.config/kanon
```

Use `KANON_HOME` or `--home` to point at the settings repository. Use `kanon pull` and `kanon push` for explicit git sync.

## Managed settings

Kanon can render:

- instructions into `AGENTS.md` and `CLAUDE.md`
- skills into Codex and Claude skill directories
- MCP server definitions
- hooks

The default flow is preview first, then apply. Existing unmanaged files block writes unless `--adopt` is passed, and overwritten files are backed up under `.kanon/backups`.

## Importing existing settings

```sh
kanon import --agent all
kanon import --agent all --write
kanon import --agent all --write --force
```

Import previews normalize existing Codex and Claude settings into Kanon config. Imported config is neutral by default: instructions, skills, MCP servers, and hooks are lifted into top-level sections with optional `targets` when a setting only applies to some agents. Native fields that do not map to the neutral schema are skipped with warnings, including agent permission settings, which kanon does not manage.

For now, import supports `--secret-policy keep` only. Secret-looking values are preserved and reported with warnings so you can move them to environment references or another secret manager manually. Future policies for env refs, omission, password managers, and encrypted secrets are tracked in code TODOs.

If both `AGENTS.md` and `CLAUDE.md` exist and differ, import stops by default. Re-run with `--instruction-policy codex`, `claude`, `merge`, or `skip` to choose how to create neutral instructions. `--write` refuses to replace an existing `kanon.yaml`; use `--force` when intentionally re-importing.
