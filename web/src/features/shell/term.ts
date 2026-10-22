// SPDX-License-Identifier: AGPL-3.0-only

// A small terminal model for the shell tab: applies output bytes to a line
// buffer (CR, LF, backspace, erase-line, clear-screen; other escape sequences
// are dropped) and maps keys to the bytes a terminal sends.

export class TermBuffer {
  lines: string[] = [""];
  col = 0;
  private pending = "";
  constructor(public max = 2000) {}

  write(text: string) {
    const s = this.pending + text;
    this.pending = "";
    for (let i = 0; i < s.length; i++) {
      const c = s[i];
      if (c === "\x1b") {
        const rest = s.slice(i);
        // eslint-disable-next-line no-control-regex
        const m = rest.match(/^\x1b\[([0-9;?]*)([@-~])/) || rest.match(/^\x1b\][^\x07]*(\x07|\x1b\\)/) || rest.match(/^\x1b[()][A-Za-z0-9]/) || rest.match(/^\x1b[=>78DEHMc]/);
        if (!m) {
          if (rest.length < 16 && !/[@-~]/.test(rest.slice(2))) { this.pending = rest; return; }
          continue;
        }
        if (m[0].startsWith("\x1b[")) this.csi(m[1] ?? "", m[2] ?? "");
        i += m[0].length - 1;
        continue;
      }
      if (c === "\r") { this.col = 0; continue; }
      if (c === "\n") { this.lines.push(""); this.col = 0; this.trim(); continue; }
      if (c === "\b") { this.col = Math.max(0, this.col - 1); continue; }
      if (c === "\x07") continue;
      if (c < " " && c !== "\t") continue;
      this.put(c);
    }
  }

  private put(c: string) {
    const l = this.lines[this.lines.length - 1];
    const padded = l.length < this.col ? l + " ".repeat(this.col - l.length) : l;
    this.lines[this.lines.length - 1] = padded.slice(0, this.col) + c + padded.slice(this.col + 1);
    this.col++;
  }

  private csi(args: string, cmd: string) {
    const n = Number(args.split(";")[0]) || 0;
    const last = this.lines.length - 1;
    if (cmd === "K") {
      const l = this.lines[last];
      if (n === 0) this.lines[last] = l.slice(0, this.col);
      else if (n === 1) this.lines[last] = " ".repeat(this.col) + l.slice(this.col);
      else this.lines[last] = "";
    } else if (cmd === "J" && (n === 2 || n === 3)) {
      this.lines = [""]; this.col = 0;
    } else if (cmd === "D") this.col = Math.max(0, this.col - (n || 1));
    else if (cmd === "C") this.col += n || 1;
    else if (cmd === "G") this.col = Math.max(0, (n || 1) - 1);
    else if (cmd === "P") {
      const l = this.lines[last];
      this.lines[last] = l.slice(0, this.col) + l.slice(this.col + (n || 1));
    }
    // Colours (m), cursor moves across lines and modes are ignored.
  }

  private trim() {
    if (this.lines.length > this.max) this.lines.splice(0, this.lines.length - this.max);
  }

  text() { return this.lines.join("\n"); }
}

/** Bytes to send for a key press, or null when the key is not for the terminal. */
export function keyBytes(e: { key: string; ctrlKey: boolean; altKey: boolean; metaKey: boolean }): string | null {
  if (e.metaKey) return null;
  const map: Record<string, string> = {
    Enter: "\r", Backspace: "\x7f", Tab: "\t", Escape: "\x1b", Delete: "\x1b[3~",
    ArrowUp: "\x1b[A", ArrowDown: "\x1b[B", ArrowRight: "\x1b[C", ArrowLeft: "\x1b[D",
    Home: "\x1b[H", End: "\x1b[F", PageUp: "\x1b[5~", PageDown: "\x1b[6~",
  };
  if (map[e.key]) return map[e.key];
  if (e.ctrlKey && e.key.length === 1) {
    const c = e.key.toLowerCase().charCodeAt(0);
    if (c >= 97 && c <= 122) return String.fromCharCode(c - 96);
    if (e.key === "[") return "\x1b";
    if (e.key === "\\") return "\x1c";
    return null;
  }
  if (e.key.length === 1) return (e.altKey ? "\x1b" : "") + e.key;
  return null;
}
