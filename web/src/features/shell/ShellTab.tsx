// SPDX-License-Identifier: AGPL-3.0-only

// Shell tab (docs/SPEC.md 13.3, 11.4): a terminal over the shell WebSocket.
// Binary frames carry the terminal stream; text frames carry
// {"resize":[cols,rows]} and {"exit":code}. Load it lazily.

import { useEffect, useRef, useState } from "react";
import type { Service, ShellControl } from "../../api/types.gen";
import { svcPath } from "../service/client";
import { Button, cx } from "../service/ui";
import { TermBuffer, keyBytes } from "./term";

function newSession() {
  return (crypto as any).randomUUID ? crypto.randomUUID() : Math.random().toString(36).slice(2) + Date.now().toString(36);
}

export default function ShellTab({ eid, service }: { eid: string; service: Service }) {
  const replicas = service.replicas.filter((r) => ["running", "healthy", "starting", "unhealthy"].includes(r.state));
  const [replica, setReplica] = useState(replicas[0]?.id || "");
  const [session, setSession] = useState(newSession);
  const [attempt, setAttempt] = useState(0);
  const [state, setState] = useState<"connecting" | "open" | "closed">("connecting");
  const [exit, setExit] = useState<number | null>(null);
  const [text, setText] = useState("");
  const ws = useRef<WebSocket | null>(null);
  const box = useRef<HTMLPreElement>(null);
  const buf = useRef(new TermBuffer());

  const size = () => {
    const el = box.current;
    if (!el) return { cols: 120, rows: 32 };
    return { cols: Math.max(20, Math.floor(el.clientWidth / 7.2)), rows: Math.max(5, Math.floor(el.clientHeight / 16)) };
  };

  useEffect(() => {
    if (!replica) return;
    buf.current = new TermBuffer();
    setText(""); setExit(null); setState("connecting");
    const { cols, rows } = size();
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const url = `${proto}//${location.host}/api/v1${svcPath(eid, service.id)}/shell?replica=${encodeURIComponent(replica)}&cols=${cols}&rows=${rows}&session=${encodeURIComponent(session)}`;
    const sock = new WebSocket(url);
    sock.binaryType = "arraybuffer";
    ws.current = sock;
    const dec = new TextDecoder();
    sock.onopen = () => { setState("open"); box.current?.focus(); };
    sock.onclose = () => setState("closed");
    sock.onmessage = (m) => {
      if (typeof m.data === "string") {
        try { const c = JSON.parse(m.data) as ShellControl; if (c.exit !== undefined) setExit(c.exit); } catch { /* not a control frame */ }
        return;
      }
      buf.current.write(dec.decode(m.data as ArrayBuffer, { stream: true }));
      setText(buf.current.text());
    };
    const onResize = () => {
      if (sock.readyState === WebSocket.OPEN) { const s = size(); sock.send(JSON.stringify({ resize: [s.cols, s.rows] })); }
    };
    window.addEventListener("resize", onResize);
    return () => { window.removeEventListener("resize", onResize); sock.close(); };
  }, [eid, service.id, replica, session, attempt]);

  useEffect(() => { if (box.current) box.current.scrollTop = box.current.scrollHeight; }, [text]);

  const send = (s: string) => { if (ws.current?.readyState === WebSocket.OPEN) ws.current.send(new TextEncoder().encode(s)); };

  if (!replicas.length) return <div className="text-sm text-gray-600">No running replica to open a shell in. Deploy the service first.</div>;
  return (
    <div className="flex h-[60vh] flex-col">
      <div className="mb-2 flex flex-wrap items-center gap-2 text-sm">
        {replicas.length > 1 && (
          <select aria-label="Replica" value={replica} onChange={(e) => setReplica(e.target.value)} className="h-[34px] rounded-md border border-gray-200 bg-background px-2">
            {replicas.map((r) => <option key={r.id} value={r.id}>{r.id.slice(0, 8)} on {r.hostName}</option>)}
          </select>
        )}
        <span className={cx("text-xs", state === "open" ? "text-green-700" : state === "closed" ? "text-red-600" : "text-gray-500")}>
          {state === "open" ? "Connected" : state === "closed" ? (exit !== null ? `Exited with code ${exit}` : "Disconnected") : "Connecting"}
        </span>
        {state === "closed" && <Button onClick={() => { if (exit !== null) setSession(newSession()); setAttempt((n) => n + 1); }}>Reconnect</Button>}
        <span className="ml-auto text-xs text-gray-500">The session stays for 10 minutes after you leave.</span>
      </div>
      <pre ref={box} tabIndex={0} aria-label="Terminal" data-testid="terminal"
        onKeyDown={(e) => { const b = keyBytes(e); if (b !== null) { e.preventDefault(); send(b); } }}
        onPaste={(e) => { e.preventDefault(); send(e.clipboardData.getData("text")); }}
        className="min-h-0 flex-1 overflow-auto whitespace-pre-wrap break-all rounded-md border border-gray-200 bg-black p-3 font-mono text-[12px] leading-4 text-gray-100 outline-none focus:border-gray-400">
        {text}<span className="bg-gray-300">&nbsp;</span>
      </pre>
    </div>
  );
}
