# Working rules for this repo

- **Never run state-changing git commands** — no `commit`, `push`, `add`, `stash`,
  `checkout`, `reset`, `tag`, `branch`. Leave changes in the working tree; the
  owner commits and pushes by hand. Read-only git (`status`, `diff`, `log`) is fine.
- Never publish releases (`gh release …`) or push tags unless explicitly asked in
  the same message.
- Long-running work goes to background subagents; never block the conversation.
- Never run `flowlite setup`, `flowlite run`, `flowlite uninstall`, or anything that
  triggers a macOS permission prompt unless asked.
