---
name: port-python-upstream
description: Use when maintaining this repository's Go main branch while tracking Python upstream changes, checking pending upstream commits, translating Python commits into Go behavior, renaming go/main/python branches, or marking already-ported Python commits as absorbed.
---

# Port Python Upstream

## Core Model

Use Git history as the source of truth for upstream porting state, but do not confuse ancestry bookkeeping with code migration:

- `main` is the Go product branch and must not track `upstream/main`.
- `python` is the Python upstream mirror and should track `upstream/main`.
- `python` should stay clean: update it with `git merge --ff-only upstream/main`.
- `main..python` lists Python upstream commits whose ancestry has not yet been absorbed by Go.
- Absorbing ancestry is not the same as porting behavior. A Python commit is not "done" until its applicable behavior, config, docs, security fix, tests, or version change has been translated into the Go tree or explicitly classified as intentionally irrelevant.
- Only after real porting and verification may `main` use an `ours` merge to mark the corresponding Python commit or contiguous range as absorbed without copying Python runtime files into the Go tree.
- Treat pending Python commits as candidate changes, not automatic requirements. User-visible behavior, release/version text, defaults, hard limits, and commit-message wording must pass a decision gate before implementation.

## Required Safety Checks

Before branch renames, pushes, or merges:

1. Check `git status --short` and do not overwrite user work.
2. Check `git remote -v`, `git branch -vv`, and the current branch.
3. Distinguish local branch changes from remote/default-branch changes.
4. Ask before force-pushing, deleting branches, changing remote defaults, or rewriting published history.

Do not merge Go commits into `python`. Do not use regular merge from `python` into `main` for porting markers.

Never use `git merge -s ours python` as the whole solution to "merge Python changes into main". That only records ancestry and keeps the Go tree unchanged. It is valid only as the final bookkeeping step after the Python changes have already been translated, implemented, and verified in Go.

Before coding or committing a port:

1. Check the current worktree for leftovers from earlier attempts. If the user reverted a commit, verify whether the reverted changes still exist as unstaged or staged files.
2. Extract user constraints from the current session first, especially excluded files, forbidden version strings, forbidden defaults, forbidden caps, and commit-message wording.
3. Present an upstream decision table and wait for user selection whenever the pending range contains more than one behavior, any version/release change, any default/config change, any hard limit, or any ambiguous Python-only change.
4. Do not stage excluded files such as local demo compose files, HAR files, credentials, or probe artifacts.

## Initial Branch Shape

When converting the current repository layout:

```powershell
git branch -m main python
git branch -m go main

git switch python
git branch --set-upstream-to=upstream/main python

git switch main
git branch --unset-upstream
```

Then verify:

```powershell
git branch -vv
git log --oneline main..python
```

Remote changes are separate. If the user wants `origin/main` to become Go `main`, confirm first, then use a guarded push such as `--force-with-lease` only when appropriate.

## Routine Sync Workflow

Update the Python mirror:

```powershell
git fetch upstream
git switch python
git merge --ff-only upstream/main
```

Find pending upstream work:

```powershell
git switch main
git log --oneline main..python
```

For each pending commit or contiguous range, inspect the actual diff and classify every changed behavior. Do this classification before writing Go code.

| Class | Action |
| --- | --- |
| Go behavior needed | Translate the behavior into Go code, add or update Go tests first when practical, then run targeted tests |
| Security/dependency/runtime fix | Apply the equivalent fix to the Go runtime, Docker files, configs, or dependency metadata if applicable |
| Docs/assets/config still applicable | Manually port into the Go project shape; do not blindly copy Python-only docs or paths |
| Python-only runtime | Skip implementation only with an explicit reason recorded in notes; include it in commit messages only if the user has not objected to that wording |
| Unknown | Inspect the Python diff and matching Go owner before deciding |

## Upstream Decision Table Gate

Before implementation, summarize the new upstream commits in a table and ask the user to choose what to port. This avoids silently importing upstream policy choices that may not fit the Go fork.

Use this table shape:

| Upstream commit | Change / feature | Python files touched | Go impact area | Default recommendation | User choice |
| --- | --- | --- | --- | --- | --- |
| `<sha>` | One concrete behavior, not just the commit title | Key paths only | Go files/modules likely affected | Port / Skip / Ask | Pending |

Rules:

- Split a single Python commit into multiple rows if it mixes independent behaviors, such as UI text, config defaults, hard caps, and backend behavior.
- Mark release notes, version bumps, branding, default values, concurrency caps, rate limits, or policy changes as separate rows even when bundled with useful features.
- Keep the recommendation short and technical. Example: "Port backend auto_nsfw request parsing" or "Ask: changes visible version branding."
- Do not proceed to code changes or `ours` merge until the user has selected Port/Skip for ambiguous or policy rows.
- If the user says to continue without selecting every row, proceed only with rows that are clearly applicable behavior or bug fixes; leave policy/version/default rows out unless explicitly selected.
- Preserve the selected table as the execution checklist for that port. If later evidence changes a row, update the table in chat before continuing.

Example:

| Upstream commit | Change / feature | Python files touched | Go impact area | Default recommendation | User choice |
| --- | --- | --- | --- | --- | --- |
| `ba693a8` | Add `auto_nsfw` import parameter | `tokens.py`, admin UI | admin import API, import task, UI | Port | Pending |
| `0d93147` | Update displayed version | admin header | static admin header | Ask | Pending |
| `2c24672` | Add fixed batch concurrency cap | config/admin batch | batch config and request parsing | Ask | Pending |

## Porting Workflow

For each commit or contiguous range:

1. Inspect Python diff from `python`.
2. Map each changed Python file or behavior to the Go owner on `main`.
3. Build the upstream decision table and get user selection when required by the gate above.
4. Decide whether each selected change is applicable, already present, needs a Go translation, or is intentionally Python-only.
5. Add or update Go tests first when behavior changes and the test surface exists.
6. Implement only the selected Go equivalents without importing Python-only runtime code or replacing the Go product tree with Python files.
7. Scan for forbidden strings, excluded files, unintended defaults, and hard-coded caps before staging.
8. Run targeted tests, then `go test ./...` when practical.
9. Commit the Go migration on `main` with a concise message that names the upstream range only when useful and does not include user-forbidden skip wording.
10. Only after steps 1-9 are complete, mark the Python commit or contiguous range as absorbed with an `ours` merge on `main`.

Example:

```powershell
git switch main
git show --stat <python-sha>
git show <python-sha> -- <relevant-python-files>
# edit Go code, docs, config, and tests with the equivalent behavior
go test ./app/products/web/admin ./app/dataplane/reverse/protocol -count=1
git commit -m "fix(auth): port Python upstream <python-sha> auth flow"

git merge --no-ff -s ours <python-sha> -m "chore(upstream): mark Python upstream <python-sha> as absorbed after Go port"
git log --oneline main..python
```

`-s ours` records the merge relationship but keeps the Go tree content from `main`.

## Commit Message Hygiene

Commit messages are part of the public project history. Keep them neutral and avoid exposing internal deliberation the user did not ask to publish.

- Include what was ported, the Go behavior changed, and the verification commands.
- Do not include rejected upstream policy details when the user asks not to mention them.
- Do not include sensitive artifact names, credential details, SSO/cookie/token material, HAR contents, or local-only probe details.
- If a skip reason matters for future agents but should not be in public history, keep it in the chat summary or local notes, not in the commit message.
- For `ours` marker commits, prefer a neutral message such as "mark Python upstream <sha> as absorbed" plus "Go behavior was ported in <sha>". Avoid restating every skipped row unless the user explicitly wants that in history.

If a just-created local commit has the wrong message and has not been pushed, rewrite only the message. Re-verify:

```powershell
git log -2 --format="%H%n%P%n%B%n---END---"
git log -2 --format="%B" | rg "<forbidden wording>"
git log --oneline main..python
git status --short
```

## Important Limitation

Git merge ancestry is transitive. If you `ours` merge a later Python commit, Git also treats its ancestors as merged. Therefore:

- Prefer processing pending Python commits in order.
- If skipping a middle commit, explicitly decide whether its ancestors should also be considered absorbed.
- For a batch, only use an `ours` merge at the end of a contiguous range when every commit in that range has been ported or intentionally skipped.

## Common Mistakes

- Regular-merging `python` into `main`: this can bring Python runtime files into the Go branch.
- Using `ours` merge before porting: this makes `main..python` empty while silently dropping the requested Python behavior.
- Updating `python` with non-fast-forward local commits: this destroys its value as an upstream mirror.
- Treating `main..python` as "not implemented" after doing local Go code but before the `ours` marker: the implementation and marker must both happen.
- Marking a commit absorbed before tests show the Go behavior matches.
- Saying "merged" when only ancestry changed. Report this as "marked absorbed" unless code/docs/config changes were actually ported into `main`.
- Porting release/version bumps, README changelogs, default changes, hard caps, or branding changes just because they appear in upstream.
- Adding fixed limits while porting a feature unless the user selected that policy row.
- Recording user-rejected skip details in commit messages.
- Assuming a reverted commit left the worktree clean. Always re-check `git status --short` and scan the diff.
