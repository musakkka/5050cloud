// SPDX-License-Identifier: AGPL-3.0-only

// Raw Editor parsing (docs/SPEC.md 13.3). The server parses `raw` too; this
// shows the user the parsed result before they apply it.

export const NAME_RE = /^[A-Za-z_][A-Za-z0-9_]{0,255}$/;

export interface ParseResult {
  vars: Record<string, string>;
  errors: string[];
  format: "json" | "env" | "empty";
}

/** Parse pasted `.env` text or a JSON object of name to value. */
export function parseRaw(text: string): ParseResult {
  const t = text.trim();
  if (!t) return { vars: {}, errors: [], format: "empty" };
  if (t.startsWith("{")) return parseJSON(t);
  return parseEnv(text);
}

function parseJSON(t: string): ParseResult {
  const errors: string[] = [];
  const vars: Record<string, string> = {};
  let obj: unknown;
  try { obj = JSON.parse(t); } catch (e) {
    return { vars, errors: [`Not valid JSON: ${(e as Error).message}`], format: "json" };
  }
  if (!obj || typeof obj !== "object" || Array.isArray(obj)) {
    return { vars, errors: ["JSON must be an object of names to values"], format: "json" };
  }
  for (const [k, v] of Object.entries(obj as Record<string, unknown>)) {
    if (!NAME_RE.test(k)) { errors.push(`"${k}" is not a valid variable name`); continue; }
    if (v === null || typeof v === "object") { errors.push(`${k}: value must be a string, number or boolean`); continue; }
    vars[k] = String(v);
  }
  return { vars, errors, format: "json" };
}

function unescapeDouble(s: string): string {
  return s.replace(/\\([nrt"\\$])/g, (_, c: string) => ({ n: "\n", r: "\r", t: "\t" } as Record<string, string>)[c] ?? c);
}

function parseEnv(text: string): ParseResult {
  const errors: string[] = [];
  const vars: Record<string, string> = {};
  const lines = text.replace(/\r\n?/g, "\n").split("\n");
  for (let i = 0; i < lines.length; i++) {
    let line = lines[i].trim();
    if (!line || line.startsWith("#")) continue;
    if (line.startsWith("export ")) line = line.slice(7).trim();
    const eq = line.indexOf("=");
    if (eq <= 0) { errors.push(`Line ${i + 1}: expected NAME=value`); continue; }
    const name = line.slice(0, eq).trim();
    let rest = line.slice(eq + 1).trimStart();
    if (!NAME_RE.test(name)) { errors.push(`Line ${i + 1}: "${name}" is not a valid variable name`); continue; }
    const q = rest[0];
    if (q === '"' || q === "'" || q === "`") {
      // Quoted value, possibly spanning lines.
      let body = rest.slice(1);
      let end = findClose(body, q);
      while (end < 0 && i + 1 < lines.length) {
        i++;
        body += "\n" + lines[i];
        end = findClose(body, q);
      }
      if (end < 0) { errors.push(`${name}: missing closing ${q}`); continue; }
      const v = body.slice(0, end);
      vars[name] = q === '"' ? unescapeDouble(v) : v;
      continue;
    }
    // Unquoted: strip an inline comment that follows whitespace.
    const hash = rest.search(/\s#/);
    if (hash >= 0) rest = rest.slice(0, hash);
    vars[name] = rest.trim();
  }
  return { vars, errors, format: "env" };
}

function findClose(s: string, q: string): number {
  for (let i = 0; i < s.length; i++) {
    if (s[i] === "\\" && q === '"') { i++; continue; }
    if (s[i] === q) return i;
  }
  return -1;
}

/** Render variables as `.env` text for the Raw Editor. Sealed values are left out. */
export function toEnv(vars: Record<string, string>): string {
  return Object.entries(vars)
    .map(([k, v]) => (/[\s"'#\\]/.test(v) || v === "" ? `${k}="${v.replace(/\\/g, "\\\\").replace(/"/g, '\\"').replace(/\n/g, "\\n")}"` : `${k}=${v}`))
    .join("\n");
}

/** Find the `${{` reference being typed at `pos`, if any: returns its start and the partial text. */
export function refAt(value: string, pos: number): { start: number; partial: string } | null {
  const before = value.slice(0, pos);
  const start = before.lastIndexOf("${{");
  if (start < 0) return null;
  const partial = before.slice(start + 3);
  if (partial.includes("}}")) return null;
  return { start, partial: partial.trimStart() };
}

/** Template functions offered by autocomplete (9.x reference syntax). */
export const REF_FUNCTIONS = ["secret(32)", "randomInt(1000, 9999)", "shared."];

/** Suggestions for a partial reference: services first, then `Service.VAR`. */
export function refSuggestions(partial: string, services: { name: string; vars: string[] }[]): string[] {
  const p = partial.toLowerCase();
  const dot = partial.indexOf(".");
  if (dot < 0) {
    return [...services.map((s) => `${s.name}.`), ...REF_FUNCTIONS].filter((x) => x.toLowerCase().startsWith(p));
  }
  const svc = partial.slice(0, dot);
  const s = services.find((x) => x.name.toLowerCase() === svc.toLowerCase());
  if (!s) return [];
  return s.vars.map((v) => `${s.name}.${v}`).filter((x) => x.toLowerCase().startsWith(p));
}
