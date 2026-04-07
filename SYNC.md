# Repository Sync Workflow

## Repository Setup

Workshop has a two-repo architecture:

- **yuna** (`github.com/jamesnhan/yuna`) — Personal version (unsanitized, AUTHORITATIVE)
  - The source of truth for all features
  - MUST NOT be broken by workshop changes
  - Can contain personal kanban data, session info

- **workshop** (`github.com/jamesnhan/workshop`) — Work version (sanitized)
  - Downstream of yuna
  - NO personal data allowed
  - Tracks yuna features but sanitized for work use

## Git Remotes

```bash
origin → github.com/jamesnhan/workshop (personal)
yuna   → github.com/jamesnhan/yuna (work, authoritative)
```

## Sync Workflow

### Pull Features from Yuna (Common)

When yuna gets new features/fixes, pull them into workshop:

```bash
git fetch yuna
git merge yuna/main -m "Merge features from yuna"
# Sanitize any personal references (if any leaked through)
# Fix any yuna→workshop naming conflicts
git push origin main
```

### Push Features to Yuna (CAREFUL - Don't Break Yuna!)

When workshop has a well-tested feature yuna should have:

```bash
# RARE: Only push thoroughly tested, non-breaking features
# REVIEW: Ensure it won't break yuna's functionality
# REVIEW: Ensure no work-specific data in commits
git push yuna main
```

**Key Rule:** Yuna is authoritative and must NEVER be broken by workshop changes.

## What Gets Synced

**✅ Sync (via Git):**
- Code changes (features, bug fixes, UI improvements)
- Documentation
- Build configs
- Tests

**❌ Never Sync to Workshop (Sanitize):**
- Personal kanban data (SQLite stays local per repo)
- Personal session names/info
- Personal project references
- Any personal information

**❌ Never Push to Yuna:**
- Work-specific references
- Company data
- Anything that could break yuna

## Important Notes

- **Yuna is authoritative** — Personal repo, source of truth, MUST NOT BE BROKEN
- **Workshop is downstream** — Work repo, sanitized, pulls from yuna
- **Claude handles git** — Claude Code will manage git operations 99.99% of the time
- **Separate databases** — Each repo has independent SQLite kanban database (personal vs work tasks)
- **Manual review** — Always review commits before pushing to yuna
- **Never break yuna** — Workshop changes must be tested before pushing upstream

## Maintenance

After pulling from yuna, always:
1. Check for naming conflicts (yuna vs workshop)
2. Rebuild: `make build`
3. Test key features
4. Push to origin: `git push origin main`
