// SPDX-License-Identifier: AGPL-3.0-only
import { describe, expect, it } from "vitest";
import { TermBuffer, keyBytes } from "./term";

describe("TermBuffer", () => {
  it("handles text, newlines, colours and carriage returns", () => {
    const t = new TermBuffer();
    t.write("\x1b[32mhello\x1b[0m\r\nworld\rW");
    expect(t.text()).toBe("hello\nWorld");
  });
  it("handles backspace, erase line and clear screen", () => {
    const t = new TermBuffer();
    t.write("abc\b\x1b[K");
    expect(t.text()).toBe("ab");
    t.write("\x1b[2Jnew");
    expect(t.text()).toBe("new");
  });
  it("buffers a split escape sequence", () => {
    const t = new TermBuffer();
    t.write("a\x1b[3");
    t.write("1mb");
    expect(t.text()).toBe("ab");
  });
  it("drops OSC title sequences", () => {
    const t = new TermBuffer();
    t.write("\x1b]0;title\x07$ ");
    expect(t.text()).toBe("$ ");
  });
  it("keeps at most max lines", () => {
    const t = new TermBuffer(3);
    t.write("1\n2\n3\n4\n5");
    expect(t.lines).toEqual(["3", "4", "5"]);
  });
});

describe("keyBytes", () => {
  const k = (key: string, mods: Partial<{ ctrlKey: boolean; altKey: boolean; metaKey: boolean }> = {}) =>
    keyBytes({ key, ctrlKey: false, altKey: false, metaKey: false, ...mods });
  it("maps keys", () => {
    expect(k("a")).toBe("a");
    expect(k("Enter")).toBe("\r");
    expect(k("c", { ctrlKey: true })).toBe("\x03");
    expect(k("ArrowUp")).toBe("\x1b[A");
    expect(k("v", { metaKey: true })).toBeNull();
    expect(k("Shift")).toBeNull();
  });
});
