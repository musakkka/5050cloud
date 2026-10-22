# WP8b: add @xterm/xterm to web/package.json for the Shell tab

- From: WP8b
- To: integrator
- Files: web/package.json, web/package-lock.json
- Blocking: no (the Shell tab ships with a small built-in terminal in web/src/features/shell/term.ts)

## Why
docs/SPEC.md 13.3 asks for an xterm.js terminal over the shell WebSocket,
lazy-loaded. web/package.json is owned by WP8a and has no xterm dependency.
The v0.1 Shell tab works without it (real WebSocket, binary frames, resize and
exit control frames, key mapping, basic escape handling), but full-screen
programs such as vim or top need a real terminal emulator.

## Proposed change
Add `"@xterm/xterm": "5.5.0"` and `"@xterm/addon-fit": "0.10.0"` to
dependencies. WP8b then swaps the `<pre>` renderer in ShellTab.tsx for an
xterm instance inside the same lazy chunk.
