# Repository Sync Workflow

## Repository Setup

Workshop has a two-repo architecture:

- **workshop** (`github.com/jamesnhan/workshop`) — Personal version (unsanitized, AUTHORITATIVE)
  - The source of truth for all features
  - MUST NOT be broken by workshop changes
  - Can contain personal kanban data, session info

- **workshop** (`github.com/jamesnhan/workshop`) — Work version (sanitized)
  - Downstream of workshop
  - NO personal data allowed
  - Tracks workshop features but sanitized for work use

## Git Remotes

```bash
origin → github.com/jamesnhan/workshop (personal)
workshop   → github.com/jamesnhan/workshop (work, authoritative)
```

## Sync Workflow

### Pull Features from Workshop (Common)

When workshop gets new features/fixes, pull them into workshop:

```bash
git fetch workshop
git merge workshop/main -m "Merge features from workshop"
# Sanitize any personal references (if any leaked through)
# Fix any workshop→workshop naming conflicts
git push origin main
```

### Push Features to Workshop (CAREFUL - Don't Break Workshop!)

When workshop has a well-tested feature workshop should have:

```bash
# RARE: Only push thoroughly tested, non-breaking features
# REVIEW: Ensure it won't break workshop's functionality
# REVIEW: Ensure no work-specific data in commits
git push workshop main
```

**Key Rule:** Workshop is authoritative and must NEVER be broken by workshop changes.

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

**❌ Never Push to Workshop:**
- Work-specific references
- Company data
- Anything that could break workshop

## Important Notes

- **Workshop is authoritative** — Personal repo, source of truth, MUST NOT BE BROKEN
- **Workshop is downstream** — Work repo, sanitized, pulls from workshop
- **Claude handles git** — Claude Code will manage git operations 99.99% of the time
- **Separate databases** — Each repo has independent SQLite kanban database (personal vs work tasks)
- **Manual review** — Always review commits before pushing to workshop
- **Never break workshop** — Workshop changes must be tested before pushing upstream

## Maintenance

After pulling from workshop, always:
1. Check for naming conflicts (workshop vs workshop)
2. Rebuild: `make build`
3. Test key features
4. Push to origin: `git push origin main`
