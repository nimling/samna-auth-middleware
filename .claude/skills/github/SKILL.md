---
name: github
description: Manages GitHub issues, epics, sub-issues, pull requests, labels, project board, and all gh CLI interactions. Use when creating issues, filing bugs, listing or viewing issues, managing epics, viewing or updating the project board, setting labels or priority, adding sub-issues, creating or reviewing PRs, running any gh command, or any interaction with GitHub.
allowed-tools: Bash(gh *), Bash(git *)
---

# GitHub — Samna Auth Middleware

Repository: `nimling/samna-auth-middleware`
Default branch: `main`

## Argument Routing

| Argument | Action |
|----------|--------|
| (empty) or `status` | List open issues |
| `create` | Create a new issue |
| `view [number]` | View issue details |
| `close [number]` | Close an issue |
| `edit [number]` | Edit an issue |
| `labels` | List available labels |
| `board` | Show project board items and statuses |
| `mine` | List issues assigned to current user |
| `pr` | Create a PR for current branch |
| `branch [number]` | Create a branch for an issue |

## Output Format

After every operation that creates, updates, closes, lists issues, or creates pull requests, present results in this table:

| # | Title | Link |
|---|-------|------|

Use the issue or PR number, the clean title, and the full URL.

## Commands

### List open issues
```bash
gh issue list --repo nimling/samna-auth-middleware
```

### View an issue
```bash
gh issue view <NUMBER> --repo nimling/samna-auth-middleware
```

### Close an issue
```bash
gh issue close <NUMBER> --repo nimling/samna-auth-middleware
```

### Edit an issue
```bash
gh issue edit <NUMBER> --repo nimling/samna-auth-middleware --title "..." --body "..."
```

### Create an issue
```bash
gh issue create --repo nimling/samna-auth-middleware --title "..." --body "..."
```

### Push current branch
```bash
git push -u origin HEAD
```

### Create a pull request
```bash
gh pr create --repo nimling/samna-auth-middleware --title "..." --body "$(cat <<'EOF'
## Summary
<bullet points describing the changes>

Closes #<number>
EOF
)"
```

PR body contains only a Summary section and optionally a Closes line. Nothing else.

## Branch Naming

Determine branch prefix from context:
- Bug fix: `fix/{slug}`
- Feature: `feat/{slug}`
- If linked to an issue: `fix/{issue-number}-{slug}` or `feat/{issue-number}-{slug}`

Slugs are lowercase, hyphen-separated, max 5 words.

## Rules

- Clean titles only, no type prefix (no "Bug: ...", "feat: ...")
- Never create a PR or push without explicit user consent
- Always output results in the table format above
- No icons or emojis anywhere: not in issue titles, PR titles, PR bodies, commit messages, labels, or descriptions
- Never add "Generated with Claude Code", "Co-Authored-By", or any AI attribution to PRs, commits, or issues
- PRs have only a Summary section and optionally a Closes line. No test plan, no checklist, no footer, no icons
- All git operations use the current system git user identity, never override it
