-- Workshop configuration for the Workshop project itself
-- This file is loaded via command palette "Load Config" or `workshop --config workshop.lua`

-- Set theme
workshop.theme("catppuccin-mocha")

-- Grid layout: 1x2 (code left, agent right)
workshop.grid(1, 2)

-- Define sessions to create/attach
workshop.session("workshop", "~/repos/workshop")

-- Agent presets available in the launcher
workshop.agent({
  name = "code-reviewer",
  model = "sonnet",
  directory = "~/repos/workshop",
  prompt = "Review the recent changes in this repo for bugs, security issues, and code quality. Focus on the Go backend and React frontend.",
})

workshop.agent({
  name = "test-writer",
  model = "sonnet",
  directory = "~/repos/workshop",
  prompt = "Write tests for any untested code. Focus on the tmux bridge and API handlers.",
})

workshop.agent({
  name = "doc-writer",
  model = "haiku",
  directory = "~/repos/workshop",
  prompt = "Generate a comprehensive README.md for this project based on the codebase.",
})

-- Dynamic: check if we have existing sessions
local sessions = workshop.list_sessions()
if sessions then
  workshop.log("Found " .. #sessions .. " existing sessions")
end

-- Environment-aware config
local env = workshop.env("WORKSHOP_ENV")
if env == "prod" then
  workshop.log("Production mode — skipping dev agents")
else
  workshop.log("Development mode")
end
