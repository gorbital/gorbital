# Git in the Dev Portal

The Dev Portal's Git screen ([Dev Portal guide](dev-portal.md), [ADR-0076](../adr/0076-git-screen.md)) works on the app's repository with the `git` installed on your machine: `orb dev` runs it in the app directory, so your hooks, commit signing, credential helper, SSH agent and configuration apply exactly as in the terminal.

## What it does

| Screen | Commands behind it |
|---|---|
| Status: branch, upstream, ahead and behind, changed files | `git status --porcelain=v2 --branch` |
| Diff viewer, stage and unstage per file or per hunk, discard | `git diff`, `git add`, `git restore --staged`, `git apply --cached [-R]`, `git restore` and `git clean -f` for discards |
| Commit | `git commit --file=-` with your message |
| Branches: create, switch, delete; remote branch names | `git branch`, `git switch`, `git branch -d` (or `-D` after you have seen the unmerged commits) |
| Fetch, pull, push | `git fetch --all --prune`, `git pull --no-rebase`, `git push` (`--set-upstream origin <branch>` the first time) |
| Merge preview, merge, abort; conflicts | `git merge-tree --write-tree` (Git 2.38+), `git merge --no-edit`, `git merge --abort`, `git diff --diff-filter=U` |
| Log and graph | `git log --all` with parents and refs |
| Open a conflicted file | your editor: `ORB_EDITOR` or `VISUAL` (a command that takes `path:line`), else `code --goto`, else the system opener |

Git's own refusals appear as they are (409 `git_refused` with git's message): a switch that would lose changes, a commit with nothing staged, a push the remote rejects.

## What it never does

- Rewrite history: no rebase, no amend, no reset.
- Push with force, or delete a remote branch.
- Answer a credential prompt: `GIT_TERMINAL_PROMPT=0`; set up a credential helper or an SSH agent, as you would for a script.

Destructive actions git allows are confirmed with what they lose: discarding shows the diff, deleting an unmerged branch lists its commits, aborting a merge says what it drops.

## Endpoints

All under `/_portal/api/git/`, guarded like the rest of the portal (loopback, the run's token, the `X-Orb-Portal` header on writes): `status`, `diff?path=&staged=`, `stage`, `unstage`, `discard`, `patch`, `commit`, `branches` (GET and POST), `switch`, `unmerged?branch=`, `branches/{name}` (DELETE, `?force=true`), `fetch`, `pull`, `push`, `merge/preview?branch=`, `merge`, `merge/abort`, `conflicts`, `log?all=&range=&path=&limit=&skip=`, `open`. Paths must stay inside the repository and can't start with `-`.
