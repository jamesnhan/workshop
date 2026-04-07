# Update Workshop

Sync workshop with latest changes from yuna (authoritative source).

## Workflow

Execute the following steps to update workshop:

1. **Fetch from origin:**
   ```bash
   git fetch origin
   ```

2. **Show what's changed:**
   ```bash
   git log --oneline HEAD..origin/main -10
   git diff --stat HEAD origin/main
   ```
   Display the commits and file changes to the user.

3. **Ask for confirmation:**
   Show the user what will change and ask: "Ready to update workshop with these changes?"

4. **If confirmed, reset to origin/main:**
   ```bash
   git reset --hard origin/main
   ```

5. **Rebuild and install:**
   ```bash
   make install
   ```

6. **Restart workshop:**
   ```bash
   # Kill running workshop server (not MCP servers)
   kill $(pgrep -f "workshop$" | grep -v "workshop mcp") 2>/dev/null
   sleep 1

   # Start workshop from installed binary
   nohup workshop > /tmp/workshop.log 2>&1 & disown
   sleep 2

   # Verify it's running
   pgrep -fl workshop
   ```

7. **Verify:**
   ```bash
   curl -s http://localhost:9090 | head -5
   git status
   ```

   Report success and remind user to refresh browser.

## Important Notes

- **origin** is github.com/jamesnhan/workshop (this repo, work/sanitized)
- **yuna** is github.com/jamesnhan/yuna (personal, authoritative source)
- Yuna pushes updates to workshop's origin, not to the yuna remote
- This updates workshop with yuna's latest features
- Always show changes before resetting (safety check)
- Workshop MCP servers are not restarted (they stay running)

## When to Use

Use this skill when:
- Yuna has pushed updates to workshop
- You need to sync the latest features from yuna
- After yuna says "I pushed to origin, run update-workshop"
