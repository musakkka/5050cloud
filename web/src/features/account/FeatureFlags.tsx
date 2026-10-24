// SPDX-License-Identifier: AGPL-3.0-only

import { useSyncExternalStore } from "react";
import { Switch, readPref, writePref } from "../../kit";

export interface Flag { id: string; title: string; description: string }

/** Console-only preferences, kept in this browser. Features read them with useFlag(id). */
export const FLAGS: Flag[] = [
  { id: "denseTables", title: "Compact tables", description: "Tighter rows in lists and tables across the console." },
  { id: "showIds", title: "Show identifiers", description: "Show service, deployment and environment ids next to names for scripting." },
  { id: "confirmQuiet", title: "Skip low-risk confirmations", description: "Do not ask before restarting or redeploying a service." },
  { id: "reduceMotion", title: "Reduce motion", description: "Turn off the slide, scale and shimmer animations." },
];

const KEY = "5050.flags";
const subs = new Set<() => void>();
let cache: Record<string, boolean> = readPref(KEY, {});
const snapshot = JSON.stringify;

export function setFlag(id: string, on: boolean) {
  cache = { ...cache, [id]: on };
  writePref(KEY, cache);
  document.documentElement.toggleAttribute("data-reduce-motion", !!cache.reduceMotion);
  subs.forEach((f) => f());
}

export function useFlag(id: string): boolean {
  const v = useSyncExternalStore((f) => { subs.add(f); return () => subs.delete(f); }, () => snapshot(cache));
  return !!(JSON.parse(v) as Record<string, boolean>)[id];
}

export default function FeatureFlags() {
  return (
    <div className="space-y-0">
      <p className="mb-6 text-body text-fg-2">Experimental switches for this browser. They are saved locally and do not change other people's console.</p>
      <hr className="border-0 border-t border-gray-100" />
      <ul>
        {FLAGS.map((f) => <FlagRow key={f.id} flag={f} />)}
      </ul>
    </div>
  );
}

function FlagRow({ flag }: { flag: Flag }) {
  const on = useFlag(flag.id);
  return (
    <li className="flex items-start gap-6 border-b border-gray-100 py-6">
      <Switch checked={on} onChange={(v) => setFlag(flag.id, v)} label={flag.title} className="mt-0.5" />
      <div className="max-w-[810px]">
        <div className="text-lead font-semibold">{flag.title}</div>
        <p className="text-body text-fg-2">{flag.description}</p>
      </div>
    </li>
  );
}
