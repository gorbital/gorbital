# ADR-0076: The Dev Portal's Git screen

**Status:** Accepted (2026-09-17) · **Amends:** ADR-0066

## Context

Phase 11 of the [Dev Portal roadmap](../dev-portal-roadmap.md) is the Git screen: status, a diff viewer with staging by file and hunk, commits, branches, fetch, pull and push, a merge preview, conflicts opened in the editor, the log with a graph, and a confirmation naming what every destructive action loses. `orb` already runs `git` for its own checks (a clean tree before generators write).

## Options

| Option | Verdict |
|---|---|
| A Go git implementation (go-git) inside `orb` | Rejected: hooks, commit signing, credential helpers, SSH agents, `includeIf` configuration and LFS behave differently or not at all; the developer's git is the truth |
| **The developer's `git` binary, run by `orb dev` in the app directory (`cli/internal/portal/git.go`): `GIT_TERMINAL_PROMPT=0` (a helper or agent answers, or the command fails with its message), `GIT_EDITOR=true`, `LC_ALL=C` for parseable output, 30 s per command and 2 min for the network ones; porcelain formats (`status --porcelain=v2`, `for-each-ref`, `log --format` with separators) parsed into JSON** | **Chosen**: what the terminal does, the portal does; nothing to keep in sync |
| Hunk staging by line ranges | Rejected: `git apply --cached` with the hunk's patch (and `-R` to unstage) is what `git add -p` does, and the diff viewer already has the hunk text |
| Merge preview by merging in a temporary worktree | Rejected: `git merge-tree --write-tree` (Git 2.38+) reports the conflicted files without touching anything |

### What the portal never does

Rewrite history (`rebase`, `commit --amend`, `reset --hard`), push with force, delete a remote branch, or run hooks differently. Destructive operations that git itself allows are exposed with what they lose: discarding changes (the diff is shown first), deleting an unmerged branch (`force` only after the unmerged commits are listed), aborting a merge.

## Decision

| Piece | Decision |
|---|---|
| `portal.Git` | `Status` (branch, upstream, ahead/behind, files with index and worktree letters, conflicts, the operation in progress), `Diff` (index vs worktree or HEAD vs index; untracked files whole), `Stage`, `Unstage`, `ApplyPatch` (hunks), `Discard`, `Commit` (message by stdin, `--cleanup=strip`), `Branches` (local with upstream, ahead/behind, merged; remote names), `CreateBranch`, `Switch`, `DeleteBranch` (`-d`, `-D` with force), `UnmergedCommits`, `Fetch` (`--all --prune`), `Pull` (`--no-rebase --no-edit`, conflicts listed), `Push` (sets the upstream when missing; never force), `MergePreview`, `Merge`, `AbortMerge`, `Conflicts`, `Log` (parents and refs for the graph; `--all`, a range, a path, paging) |
| Endpoints | `GET /_portal/api/git/status`, `git/diff?path=&staged=`, `POST git/stage`, `git/unstage`, `git/discard` (`{paths}`), `POST git/patch` (`{patch, reverse}`), `POST git/commit` (`{message}`), `GET|POST git/branches`, `POST git/switch`, `GET git/unmerged?branch=`, `DELETE git/branches/{name}?force=`, `POST git/fetch`, `git/pull`, `git/push`, `GET git/merge/preview?branch=`, `POST git/merge`, `git/merge/abort`, `GET git/conflicts`, `GET git/log?all=&range=&path=&limit=&skip=`, `POST git/open` (`{path, line}`: the developer's editor, `ORB_EDITOR` or `VISUAL`, else `code --goto`, else the system opener) |
| Errors | git's own refusals answer 409 `git_refused` with git's message; paths that leave the repository or look like options 400 `invalid_git_request`; a directory without a repository 404 `not_a_repository` |
| Safety | Paths are validated before they reach git (no leading `-`, `/` or `..`); branch names by pattern and `check-ref-format`; every write needs the portal's mutation header; no force, no rewrite |
| The screen | Status with branch and ahead/behind, changed files with stage/unstage per file and hunk, diff viewer, commit box, branches with create/switch/delete, fetch/pull/push, merge preview then merge, conflicts with "open in editor", the log graph; every destructive action confirmed with what it loses |

## Why

- The developer's git is configured for their remotes, signing and hooks; running it keeps the portal honest.
- Porcelain formats are stable and made for this; parsing them is a few functions with tests against a real repository.
- Refusing force and rewrites in the API (not just the UI) keeps a wrong click from losing work.

## Trade-offs

- Git 2.38 or newer for the merge preview (older gits answer an error for the preview; merging still works).
- Credentials that would prompt fail instead; the developer sets up a helper or an agent, as for CI.
- The screen works on the app's repository only (the directory `orb dev` runs in), not on nested ones.

## Consequences

- Threat model ([ADR-0029](0029-threat-model.md)): the git endpoints run a program with the developer's credentials, behind the portal's guard (loopback, token, mutation header); inputs are validated as arguments, never concatenated into a shell.
- Guides: [git](../guides/git.md) (new), [Dev Portal](../guides/dev-portal.md).

## Implementation notes (2026-09-17)

`TestGitWorkflow` (a temporary repository with a bare remote: status, diffs, staging by file and patch, discard, branches, commit, merge preview and merge, push, fetch, pull, log, a conflicting merge and its abort, forced delete) and `TestGitEndpoints` in `cli/internal/portal`.
