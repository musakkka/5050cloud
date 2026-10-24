// SPDX-License-Identifier: AGPL-3.0-only

// `npm run dev:mocks` only (loaded by dynamic import, so production builds drop
// it): a fake shell that prints a prompt, echoes typing and answers a few
// commands, and an in-memory file tree for the Files drawer.

import { ApiError } from "../service/client";
import type { FileEntry, Listing } from "./files";
import type { Link, LinkHandlers } from "./link";

const enc = new TextEncoder();
const PROMPT = "root@1996653ea390:/app# ";
const CWD = "/app";
const now = Math.floor(Date.now() / 1000);

const dir = (name: string, ago: number): FileEntry => ({ name, type: "dir", size: 0, modified: now - ago });
const file = (name: string, size: number, ago: number): FileEntry => ({ name, type: "file", size, modified: now - ago });

const TREE: Record<string, FileEntry[]> = {
  "": [
    dir("src", 3600),
    dir("scripts", 7200),
    dir("data", 86400),
    file(".env.example", 982, 9000),
    file("README.md", 6600, 9000),
    file("app.py", 4800, 9100),
    file("pyproject.toml", 874, 9100),
    file("run.sh", 843, 9100),
    file("config.json", 2100, 9100),
    file("uv.lock", 207600, 9100),
  ],
  src: [
    file("__init__.py", 23, 9500),
    file("server.py", 21800, 9500),
    file("routes.py", 4700, 9500),
    file("cli.py", 3900, 9500),
    dir("templates", 9600),
  ],
  "src/templates": [file("index.html", 1480, 9800)],
  scripts: [file("seed.sh", 412, 9800)],
  data: [],
};

export async function mockListing(path: string): Promise<Listing> {
  await delay(260);
  if (!(path in TREE)) throw new ApiError(404, "path_not_found", "No such directory.");
  return { cwd: CWD, path, entries: TREE[path] };
}

export async function mockDelete(path: string): Promise<void> {
  await delay(200);
  const cut = path.lastIndexOf("/");
  const parent = cut < 0 ? "" : path.slice(0, cut);
  const name = path.slice(cut + 1);
  TREE[parent] = (TREE[parent] ?? []).filter((e) => e.name !== name);
}

export async function mockUpload(f: File, onProgress: (pct: number) => void): Promise<void> {
  for (const pct of [20, 55, 90, 100]) {
    await delay(180);
    onProgress(pct);
  }
  TREE[""] = [...(TREE[""] ?? []), file(f.name, f.size, 0)];
}

const delay = (ms: number) => new Promise((r) => setTimeout(r, ms));

/** A fake shell link. Typed text is echoed; Enter runs a few commands. */
export function mockLink(h: LinkHandlers): Link {
  let line = "";
  let closed = false;
  const out = (s: string) => { if (!closed) h.onData(enc.encode(s)); };
  const prompt = () => out(PROMPT);

  const run = (cmd: string) => {
    const [name, ...args] = cmd.trim().split(/\s+/);
    switch (name) {
      case "": break;
      case "pwd": out(`${CWD}\r\n`); break;
      case "whoami": out("root\r\n"); break;
      case "clear": out("\x1b[2J\x1b[H"); break;
      case "ls": {
        const names = (TREE[args[0] ?? ""] ?? []).map((e) => (e.type === "dir" ? `\x1b[1;34m${e.name}/\x1b[0m` : e.name));
        out(`${names.join("  ")}\r\n`);
        break;
      }
      case "uname": out("Linux 6.8.0 x86_64\r\n"); break;
      default: out(`\x1b[31m${name}: command not found\x1b[0m\r\n`);
    }
  };

  setTimeout(() => {
    if (closed) return;
    h.onOpen();
    prompt();
  }, 220);

  return {
    send(text) {
      for (const ch of text) {
        if (ch === "\r") {
          out("\r\n");
          run(line);
          line = "";
          prompt();
        } else if (ch === "\x7f") {
          if (line.length) { line = line.slice(0, -1); out("\b \b"); }
        } else if (ch >= " ") {
          line += ch;
          out(ch);
        }
      }
    },
    control() { /* the fake shell keeps no window size */ },
    close() { closed = true; },
  };
}
