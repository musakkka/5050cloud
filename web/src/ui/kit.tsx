// SPDX-License-Identifier: AGPL-3.0-only

import { useEffect, useRef, useState } from "react";
import { AlertTriangle, CircleAlert, CircleCheck, CircleSlash, Loader2 } from "lucide-react";
import type { Status } from "../api";
import { BrandIcon, hasBrandIcon } from "../kit/brand-icons";

export const cx = (...a: (string | false | null | undefined)[]) => a.filter(Boolean).join(" ");

export function timeAgo(ts?: number | null) {
  if (!ts) return "";
  const s = Math.max(0, Date.now() / 1000 - ts);
  if (s < 45) return "just now";
  const units: [number, string][] = [[60, "minute"], [3600, "hour"], [86400, "day"], [2592000, "month"], [31536000, "year"]];
  let out = "";
  for (let i = units.length - 1; i >= 0; i--) {
    const [n, name] = units[i];
    if (s >= n * 0.92) {
      const v = Math.max(1, Math.round(s / n));
      out = `${v} ${name}${v > 1 ? "s" : ""} ago`;
      break;
    }
  }
  return out || "just now";
}

export function fmtBytes(b?: number | null, digits = 1) {
  if (b == null) return "";
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  while (b >= 1000 && i < u.length - 1) { b /= 1000; i++; }
  return `${b.toFixed(i ? digits : 0)} ${u[i]}`;
}

export const fmtDateTime = (ts: number) => {
  const d = new Date(ts * 1000);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
};

export const tzLabel = () => {
  const off = -new Date().getTimezoneOffset() / 60;
  return `GMT${off >= 0 ? "+" : ""}${off}`;
};

export function usePoll<T>(fn: () => Promise<T>, deps: unknown[], every = 10000) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const tick = useRef(0);
  const [nonce, setNonce] = useState(0);
  useEffect(() => {
    let alive = true;
    const my = ++tick.current;
    const run = () => fn().then((d) => alive && my === tick.current && (setData(d), setError(null))).catch((e) => alive && setError(String(e.message || e)));
    run();
    const t = every ? setInterval(run, every) : undefined;
    return () => { alive = false; if (t) clearInterval(t); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, nonce]);
  return { data, error, reload: () => setNonce((n) => n + 1), setData };
}

/** A service or database icon: the official logo for a known key, kind or image; otherwise an icon URL or a source glyph. */
export function ResourceIcon({ icon, kind, source, size = 20 }: { icon?: string | null; kind?: string; source?: string; size?: number }) {
  if (hasBrandIcon(icon)) return <BrandIcon name={icon} size={size} />;
  if (hasBrandIcon(kind)) return <BrandIcon name={kind} size={size} />;
  if (icon && (icon.startsWith("/") || icon.startsWith("http"))) return <img src={icon} alt="" width={size} height={size} className="shrink-0 object-contain" style={{ width: size, height: size }} />;
  if (source === "image") return <BrandIcon name="docker" size={size} />;
  if (source === "cron") return <BrandIcon name={null} fallback="clock" size={size} />;
  return <BrandIcon name="github" size={size} />;
}

export function StatusGlyph({ status, size = 20 }: { status: Status | string; size?: number }) {
  switch (status) {
    case "online":
    case "success":
      return <CircleCheck size={size} className="text-success shrink-0" strokeWidth={1.75} />;
    case "crashed":
    case "failed":
      return <CircleAlert size={size} className="text-red-600 shrink-0" strokeWidth={1.75} />;
    case "unhealthy":
    case "warning":
      return <AlertTriangle size={size} className="text-yellow-600 shrink-0" strokeWidth={1.75} />;
    case "deploying":
    case "progress":
    case "building":
      return <Loader2 size={size} className="text-blue-600 shrink-0 spin" strokeWidth={1.75} />;
    case "restarted":
      return <CircleCheck size={size} className="text-blue-600 shrink-0" strokeWidth={1.75} />;
    default:
      return <CircleSlash size={size} className="text-gray-500 shrink-0" strokeWidth={1.75} />;
  }
}

export const STATUS_TEXT: Record<string, string> = {
  online: "Online", deploying: "Deploying", crashed: "Crashed", unhealthy: "Unhealthy", removed: "Stopped", failed: "Failed",
};
export const STATUS_COLOR: Record<string, string> = {
  online: "text-green-600", deploying: "text-blue-600", crashed: "text-red-600", unhealthy: "text-yellow-600", removed: "text-gray-500", failed: "text-red-600",
};
export const DOT_COLOR: Record<string, string> = {
  online: "bg-green-500", deploying: "bg-blue-500", crashed: "bg-red-500", unhealthy: "bg-yellow-500", removed: "bg-gray-400", failed: "bg-red-500",
};

// Shared controls come from the console kit so both trees look the same.
export { Badge, Button, Confirm, Empty, Menu, MenuItem, Modal, Popover, Spinner, Tabs, useClickOutside } from "../kit";
