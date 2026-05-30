# kanon

Manage multiple coding-agent settings across multiple diverse machines.

Kanon compiles **one** neutral settings spec into the native files each coding
agent expects, and keeps those files in sync across machines. The model mirrors
[chezmoi](https://www.chezmoi.io/), with one extra step: a compiler in the
middle that fans a single source out to many agents.

## Concepts

Kanon moves your settings between three states, plus a git remote for sharing:

- **Source state** — `kanon.yaml` plus `instructions/`, `skills/`, and `hooks/`
  in the Kanon home. The single source of truth, tracked in git.
- **Target state** — the agent-native files **computed** from the source by the
  per-agent adapters (`codex`, `claude`). Never stored; recomputed on demand.
- **Destination state** — the real files on this machine.

### Set up kanon on your current machine

```mermaid
sequenceDiagram
    participant R as remote repo
    participant S as source state
    participant T as target state
    participant D as destination
    Note over S: kanon init
    D->>S: kanon import
    Note over S: edit kanon.yaml + assets
    S->>T: kanon render
    T-->>D: kanon diff
    T->>D: kanon apply
    Note over S: git commit
    S->>R: kanon push
```

### Set up another machine and keep it in sync

```mermaid
sequenceDiagram
    participant R as remote repo
    participant S as source state
    participant T as target state
    participant D as destination
    R->>S: git clone $REPO
    S->>T: kanon render
    T-->>D: kanon diff
    T->>D: kanon apply
    R->>D: kanon update
```

Every command is an arrow between two states:

| Command | Moves | Description |
|---|---|---|
| `init` | remote → source | Create (or scaffold) the source repository |
| `validate` | source | Check `kanon.yaml` and referenced assets |
| `render` | source → target | Compile and print the agent-native files |
| `diff` | target ↔ destination | Preview the changes apply would make |
| `apply` | target → destination | Write the changes to disk |
| `status` | — | Source git status and destination drift |
| `import` (alias `add`) | destination → source | Capture existing agent files into the spec |
| `update` | remote → destination | Pull, then render and apply in one step |
| `pull` / `push` | source ↔ remote | Sync the source with a git remote |

## Quick start

```sh
kanon init --home ~/.config/kanon
kanon validate --home ~/.config/kanon
kanon render --home ~/.config/kanon    # inspect the target state
kanon diff --home ~/.config/kanon      # compare target against disk
kanon apply --home ~/.config/kanon     # write to disk
```

On another machine, `kanon update` pulls the latest source and applies it in one
step. Use `KANON_HOME` or `--home` to point at the source repository, and
`kanon pull` / `kanon push` for explicit git sync.

## Managed settings

From the source state, Kanon renders:

- instructions into `AGENTS.md` and `CLAUDE.md`
- skills into Codex and Claude skill directories
- MCP server definitions
- hooks
- permissions and Codex rules

The default flow is preview first (`render` / `diff`), then `apply`. Existing
unmanaged files block writes unless `--adopt` is passed, and overwritten files
are backed up under `.kanon/backups`.

## Importing existing settings

```sh
kanon import --agent all
kanon import --agent all --write
kanon import --agent all --write --force
```

`import` runs the pipeline in reverse: it reads existing Codex and Claude files
(the destination state) and normalizes them back into the neutral source state.
Imported config is neutral by default: instructions, skills, MCP servers, hooks,
and permissions are lifted into top-level sections with optional `targets` when
a setting only applies to some agents. Native fields that do not map to the
neutral schema are skipped with warnings.

For now, import supports `--secret-policy keep` only. Secret-looking values are
preserved and reported with warnings so you can move them to environment
references or another secret manager manually. Future policies for env refs,
omission, password managers, and encrypted secrets are tracked in code TODOs.

If both `AGENTS.md` and `CLAUDE.md` exist and differ, import stops by default.
Re-run with `--instruction-policy codex`, `claude`, `merge`, or `skip` to choose
how to create neutral instructions. `--write` refuses to replace an existing
`kanon.yaml`; use `--force` when intentionally re-importing.
