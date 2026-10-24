// SPDX-License-Identifier: AGPL-3.0-only

// Settings > Hosts (SPEC 11.2): every machine in the install with its capacity, state, roles and
// notices. Filter with "/", switch list and grid, open a machine for its facts and metrics, drain,
// maintain, adopt, edit or remove it, and add one with a single-use join command.
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { LayoutGrid, Plus, Rows3, Search, Server } from "lucide-react";
import { Button, EmptyState, Input, Kbd, Listbox, Notice, SegmentedControl, fmtBytes, optimistic, plural, readPref, toastError, toastSuccess, writePref, type ListboxOption } from "../../kit";
import type * as T from "../../api/types.gen";
import { HostRow, HostRowSkeleton, HostTile, type HostActions } from "./HostCard";
import { AddHostDialog, EditHostDialog, RemoveHostDialog } from "./HostDialogs";
import { HostSheet } from "./HostSheet";
import { hostsApi, messageOf } from "./client";

export { ROLES, toggleRole } from "./model";

type StateFilter = "all" | "online" | "draining" | "maintenance" | "down";
type View = "list" | "grid";
type Dialog = { kind: "add" } | { kind: "edit"; host: T.Host } | { kind: "remove"; host: T.Host } | null;

const STATE_OPTIONS: ListboxOption<StateFilter>[] = [
  { value: "all", label: "All states" },
  { value: "online", label: "Online" },
  { value: "draining", label: "Draining" },
  { value: "maintenance", label: "Maintenance" },
  { value: "down", label: "Offline or lost" },
];
const VIEW_KEY = "5050.hosts.view";
const POLL_MS = 15000;

/** In mocks mode the Hosts fixtures are registered once before the first list call. */
const mocksReady: Promise<unknown> = import.meta.env.VITE_MOCKS === "1"
  ? import("./mocks").then((m) => m.installHostMocks()).catch(() => undefined)
  : Promise.resolve();

const matches = (h: T.Host, query: string, filter: StateFilter) => {
  if (filter === "online" && h.state !== "online") return false;
  if (filter === "draining" && h.state !== "draining") return false;
  if (filter === "maintenance" && h.state !== "maintenance") return false;
  if (filter === "down" && !["offline", "lost", "unknown"].includes(h.state)) return false;
  const terms = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  if (!terms.length) return true;
  const haystack = [h.name, h.publicIp, h.egressIp, h.meshIp, h.version, h.os, h.arch, h.state, ...h.roles].join(" ").toLowerCase();
  return terms.every((t) => haystack.includes(t));
};

const isTyping = (el: EventTarget | null) => {
  const node = el as HTMLElement | null;
  return !!node && (node.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(node.tagName));
};

export default function HostsSettings() {
  const [hosts, setHosts] = useState<T.Host[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<StateFilter>("all");
  const [view, setView] = useState<View>(() => (readPref<string>(VIEW_KEY, "list") === "grid" ? "grid" : "list"));
  const [dialog, setDialog] = useState<Dialog>(null);
  const [openId, setOpenId] = useState<string | null>(null);
  const search = useRef<HTMLInputElement>(null);

  const load = useCallback(async () => {
    await mocksReady;
    try {
      setHosts(await hostsApi.list());
      setLoadError(null);
    } catch (e) {
      setLoadError(messageOf(e));
    }
  }, []);

  useEffect(() => {
    void load();
    const t = window.setInterval(() => void load(), POLL_MS);
    return () => clearInterval(t);
  }, [load]);

  useEffect(() => writePref(VIEW_KEY, view), [view]);

  // "/" focuses the filter, as it does in the settings of the reference console.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "/" || e.metaKey || e.ctrlKey || e.altKey || isTyping(e.target)) return;
      if (document.querySelector('[role="dialog"]')) return;
      e.preventDefault();
      search.current?.focus();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const put = (h: T.Host) => setHosts((cur) => cur?.map((x) => (x.id === h.id ? h : x)) ?? cur);
  const patch = (id: string, p: Partial<T.Host>) => setHosts((cur) => cur?.map((x) => (x.id === id ? { ...x, ...p } : x)) ?? cur);
  const nameOf = (id: string) => hosts?.find((h) => h.id === id)?.name ?? id;

  const run = async (send: () => Promise<T.Host>, done: (h: T.Host) => string) => {
    try {
      const h = await send();
      put(h);
      toastSuccess(done(h));
    } catch (e) {
      toastError(e);
    }
  };

  const actions: HostActions = {
    open: (h) => setOpenId(h.id),
    edit: (h) => setDialog({ kind: "edit", host: h }),
    maintenance: (h) => {
      const on = h.state !== "maintenance";
      void optimistic(
        () => patch(h.id, { state: on ? "maintenance" : "online" }),
        async () => {
          put(await hostsApi.maintenance(h.id, on));
          toastSuccess(on ? `${h.name} entered maintenance.` : `${h.name} left maintenance.`);
        },
        () => patch(h.id, { state: h.state }),
      );
    },
    drain: (h) => {
      const on = h.state !== "draining";
      void optimistic(
        () => patch(h.id, { state: on ? "draining" : "online" }),
        async () => {
          put(await hostsApi.update(h.id, { drain: on }));
          toastSuccess(on ? `${h.name} is draining. Replicas move off it.` : `${h.name} accepts replicas again.`);
        },
        () => patch(h.id, { state: h.state }),
      );
    },
    readopt: (h) => void run(() => hostsApi.readopt(h.id), (r) => `${r.name} was adopted again.`),
    clearConflicts: (h) => void run(() => hostsApi.clearConflicts(h.id), (r) => `Cleared the identity conflicts on ${r.name}.`),
    remove: (h) => setDialog({ kind: "remove", host: h }),
  };

  const visible = (hosts ?? []).filter((h) => matches(h, query, filter));
  const sheetHost = hosts?.find((h) => h.id === openId) ?? null;
  const filtering = query.trim() !== "" || filter !== "all";

  const clearFilter = () => { setQuery(""); setFilter("all"); };

  let body: ReactNode;
  if (hosts === null) {
    body = loadError ? null : <div className="space-y-3">{[0, 1, 2].map((i) => <HostRowSkeleton key={i} />)}</div>;
  } else if (hosts.length === 0) {
    body = (
      <EmptyState icon={<Server size={24} />} title="No hosts yet"
        body="This install runs on the control machine alone. Add a machine to spread replicas across hosts."
        action={<Button variant="primary" icon={<Plus size={16} />} onClick={() => setDialog({ kind: "add" })}>Add host</Button>} />
    );
  } else if (visible.length === 0) {
    body = (
      <EmptyState title="No hosts match" body="Clear the filter to see every machine."
        action={<Button variant="ghost" onClick={clearFilter}>Clear filter</Button>} />
    );
  } else if (view === "grid") {
    body = <ul className="grid gap-4 sm:grid-cols-2">{visible.map((h) => <HostTile key={h.id} host={h} nameOf={nameOf} actions={actions} />)}</ul>;
  } else {
    body = <ul className="flex flex-col gap-3">{visible.map((h) => <HostRow key={h.id} host={h} nameOf={nameOf} actions={actions} />)}</ul>;
  }

  const online = hosts?.filter((h) => h.state === "online").length ?? 0;
  const cpus = hosts?.reduce((n, h) => n + h.cpus, 0) ?? 0;
  const mem = hosts?.reduce((n, h) => n + h.memBytes, 0) ?? 0;

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <h2 className="sr-only">Hosts</h2>
          <p className="text-body text-fg-2">Every machine in this install. Replicas run on machines with the app role.</p>
        </div>
        <Button variant="primary" icon={<Plus size={16} />} onClick={() => setDialog({ kind: "add" })}>Add host</Button>
      </header>

      {loadError && (
        <Notice tone="danger" title="Could not load hosts"
          action={<Button size="xs" variant="outline" onClick={() => void load()}>Retry</Button>}>
          {loadError}{hosts ? " Showing the last known list." : ""}
        </Notice>
      )}

      {hosts && hosts.length > 0 && (
        <>
          <div className="flex flex-wrap items-center gap-3">
            <div className="w-full sm:w-72">
              <Input ref={search} size="sm" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Filter hosts"
                aria-label="Filter hosts" icon={<Search size={16} />} suffix={<Kbd>/</Kbd>} />
            </div>
            <Listbox<StateFilter> label="Filter by state" value={filter} onChange={setFilter} options={STATE_OPTIONS} width={200} />
            <div className="ml-auto">
              <SegmentedControl<View> label="View" size={34} value={view} onChange={setView} options={[
                { value: "list", label: "List", icon: <Rows3 size={16} /> },
                { value: "grid", label: "Grid", icon: <LayoutGrid size={16} /> },
              ]} />
            </div>
          </div>
          <p className="-mt-2 text-caption text-fg-3">
            {online} of {plural(hosts.length, "host", "hosts")} online · {plural(cpus, "vCPU", "vCPU")} · {fmtBytes(mem)} memory
            {filtering && visible.length !== hosts.length && ` · showing ${visible.length}`}
          </p>
        </>
      )}

      {body}

      {dialog?.kind === "add" && (
        <AddHostDialog onClose={() => { setDialog(null); void load(); }} />
      )}
      {dialog?.kind === "edit" && (
        <EditHostDialog host={dialog.host} onClose={() => setDialog(null)}
          onSaved={(h) => { put(h); setDialog(null); toastSuccess(`Saved ${h.name}.`); }} />
      )}
      {dialog?.kind === "remove" && (() => {
        const gone = dialog.host;
        return (
          <RemoveHostDialog host={gone} onClose={() => setDialog(null)}
            onRemoved={() => {
              setHosts((cur) => cur?.filter((x) => x.id !== gone.id) ?? cur);
              if (openId === gone.id) setOpenId(null);
              setDialog(null);
            }} />
        );
      })()}
      {sheetHost && <HostSheet host={sheetHost} nameOf={nameOf} actions={actions} onClose={() => setOpenId(null)} />}
    </div>
  );
}
