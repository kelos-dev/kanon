# Project Conventions for AI Assistants

## Rules for AI Assistants
- **Use Makefile targets** instead of discovering build/test commands yourself.
- **Keep changes minimal.** Do not refactor, reorganize, or "improve" code beyond what was explicitly requested.
- **For CI workflows**, always use existing Makefile targets rather than reimplementing build logic in YAML.
- **Better tests.** Always try to add or improve tests when modifying code.
- **Logging conventions.** Start log messages with capital letters and do not end with punctuation.
- **Commit messages.** Do not include PR links in commit messages.
- **Never use `os.Getenv()` for secrets as Go `flag` defaults.** Go's `flag` package prints default values in usage/help output, which leaks secret values. Instead, use an empty default and read the env var after `flag.Parse()`.
- **Fail fast on invalid configuration.** Do not silently fall back to degraded behavior when configuration, credentials, or source data are invalid or missing. Return an error or exit immediately instead of returning nil or empty values that mask the failure.
- **Docs must match implementation, not aspiration.** When writing or updating docs, READMEs, or comments, describe only what the code actually does. Do not document unimplemented behavior, overstate guarantees, or describe validation that is not enforced. Before describing a contract, verify the code enforces it; partial enforcement should be documented as partial.
- **CLI error messages must name the affected object.** When a command fails for a named file, skill, source, lock entry, agent, or other object, include that name in the returned error so scripted and batched invocations get an actionable signal.
- **Test the happy path, not only the early-return guards.** When a handler has both early-return guards and a primary action, unit tests must include at least one positive case verifying the primary action runs with the right arguments. If the production code lacks a seam, add one so the happy path is covered.
- **Avoid vacuous substring assertions in printer/formatter tests.** When asserting a `label: value` line is emitted, match against the full `"label: value"` string or a regex, not the bare value.
- **Preserve co-owned configuration.** Kanon merges some destination files that other tools also edit. When changing render, diff, apply, import, or merge behavior, preserve unmanaged fields and entries unless the user explicitly asks to replace them.
- **Keep source, target, and destination state distinct.** Use the README terminology consistently: source state is the Kanon repository, target state is the rendered agent-native files, and destination state is the real files on disk.

## Key Makefile Targets
- `make verify` — verify formatting, modules, vet, and tests.
- `make update` — run formatters and update module metadata.
- `make test` — run unit tests.
- `make build` — build the `kanon` binary into `bin/kanon`.

## Pull Requests
- **Always follow `.github/PULL_REQUEST_TEMPLATE.md`** when creating PRs.
- Fill in every section of the template. Do not remove or skip sections; use "N/A" or "NONE" where appropriate.
- Choose exactly one `/kind` label from: `bug`, `cleanup`, `docs`, `feature`.
- If there is no associated issue, write "N/A" under the issue section.
- If the PR does not introduce a user-facing change, write "NONE" in the `release-note` block.
- If the PR introduces a user-facing feature or behavior change, write a meaningful release note describing the change.

## Directory Structure
- `main.go` — application entry point.
- `internal/cli/` — Cobra command definitions and CLI wiring.
- `internal/core/` — Kanon source, render, diff, apply, import, lock, merge, and git logic.
- `internal/tui/` — Bubble Tea terminal UI.
- `.github/workflows/` — CI workflows.
