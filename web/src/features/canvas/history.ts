// SPDX-License-Identifier: AGPL-3.0-only

/** Local undo and redo for card moves and group changes (SPEC 13.2), 50 steps. */
export class History<S> {
  private past: S[] = [];
  private future: S[] = [];
  constructor(private limit = 50) {}
  /** Records the state before a change. */
  push(before: S) {
    this.past.push(before);
    if (this.past.length > this.limit) this.past.shift();
    this.future = [];
  }
  /** Returns the state to restore, given the current one, or undefined. */
  undo(current: S): S | undefined {
    const s = this.past.pop();
    if (s !== undefined) this.future.push(current);
    return s;
  }
  redo(current: S): S | undefined {
    const s = this.future.pop();
    if (s !== undefined) this.past.push(current);
    return s;
  }
  get canUndo() { return this.past.length > 0; }
  get canRedo() { return this.future.length > 0; }
}

/** "4 services: 3 Active, 1 Crashed" for a collapsed group. */
export function groupSummary(statuses: string[], label: (s: string) => string): string {
  const counts = new Map<string, number>();
  for (const s of statuses) counts.set(label(s), (counts.get(label(s)) || 0) + 1);
  const parts = [...counts.entries()].sort((a, b) => b[1] - a[1]).map(([k, n]) => `${n} ${k}`);
  return `${statuses.length} service${statuses.length === 1 ? "" : "s"}${parts.length ? `: ${parts.join(", ")}` : ""}`;
}
