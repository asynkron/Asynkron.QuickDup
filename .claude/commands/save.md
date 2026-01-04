---
allowed-tools: Bash(git add:*), Bash(git status:*), Bash(git diff:*), Bash(git commit:*), Bash(git push:*)
argument-hint: [commit message]
description: Stage all changes, commit, and push to GitHub
---

Commit unstaged changes and push to GitHub.

### Context
- Current git status: !`git status --short`
- Current branch: !`git branch --show-current`

### Your task

1. Stage all changes with `git add -A`
2. If a commit message was provided via $ARGUMENTS, use it. Otherwise, generate a concise commit message based on the changes.
3. Create the commit
4. Push to the remote

Keep output minimal. Just confirm what was committed and pushed.
