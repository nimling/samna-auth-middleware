---
name: git
description: Performs local git operations including committing, pushing, branching, merging, diffing, stashing, and checking status. Use when committing changes, pushing code, creating branches, viewing diffs, checking git status, merging branches, or any local version control workflow.
argument-hint: [commit|branch|push|status|log|merge|diff|...] [args]
allowed-tools: Bash(git *)
---

# Git Operations — Samna Auth Middleware

Repository: `nimling/samna-auth-middleware`

## Argument Routing

Parse `$ARGUMENTS` to determine the operation:

| Argument | Action |
|----------|--------|
| (empty) or `status` | Show branch, staged/unstaged changes, recent commits |
| `commit` | Stage and commit with a well formatted message |
| `branch [name]` | Create a branch following naming conventions |
| `push` | Push current branch, set upstream if needed |
| `diff` | Show current diff (staged + unstaged) |
| `log` | Show recent commit history |
| `merge [branch]` | Merge a branch using project conventions |
| `stash` | Stash or pop changes |
| Any other git operation | Execute directly |

## Branch Naming

Determine branch prefix from context:
- Bug fix: `fix/{slug}`
- Feature: `feat/{slug}`
- If linked to an issue: `fix/{issue-number}-{slug}` or `feat/{issue-number}-{slug}`

Slugs are lowercase, hyphen-separated, max 5 words.

## Commit Messages

Format: imperative mood, lowercase start, no period, max 72 chars first line.

Structure:
```
<what changed>

<why, if not obvious from the title>
```

Do NOT prefix with `feat:`, `fix:`, `chore:`, etc.
No icons or emojis anywhere in commit messages.
Never append `Co-Authored-By` lines. Commits are made as the current git user, not as Claude.

Before committing:
1. Run `git status` and `git diff --cached` to understand what is staged
2. If nothing is staged, ask the user what to stage
3. Draft the message and show it to the user before committing
4. Never use `--no-verify` or `--amend` unless explicitly asked

## Rules

- Never force push without explicit user consent
- Never push to main/master without explicit user consent
- Never run destructive commands (`reset --hard`, `checkout .`, `clean -f`) without explicit user consent
- Always show the user what will happen before push, merge, or destructive operations
- Prefer creating new commits over amending
- No icons or emojis in any git output (commits, tags, messages)
- Never add `Co-Authored-By` or any attribution footer to commits
- Never add "Generated with Claude Code" or any AI attribution anywhere
- Always commit as the current system git user, never override author identity
