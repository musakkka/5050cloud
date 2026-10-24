// SPDX-License-Identifier: AGPL-3.0-only

// The Files drawer under the terminal: browse the instance's working directory,
// open folders through a breadcrumb, upload (picker or drop), refresh, and a
// right-click menu with paste path, download and delete. It folds to a 28 px
// bar and remembers its state and height.

import { useEffect, useRef, useState, type DragEvent, type KeyboardEvent, type PointerEvent as RPointerEvent, type ReactNode } from "react";
import { ChevronDown, ChevronRight, ChevronUp, ClipboardPaste, Download, FileText, Folder, HardDrive, RefreshCw, Trash2, Upload } from "lucide-react";
import { ApiError, errText } from "../service/client";
import { Confirm, Spinner, Tooltip, cx, fmtBytes, readPref, toast, toastError, useClickOutside, writePref } from "../../kit";
import { absPath, deleteFile, downloadHref, fileBadge, formatStamp, joinPath, listFiles, sortEntries, uploadFile, type FileEntry, type Listing } from "./files";
import { DRAWER_DEFAULT_HEIGHT, DRAWER_HEIGHT_KEY, DRAWER_KEY } from "./theme";
import "./console.css";

const MIN_H = 120;
const maxHeight = () => Math.max(MIN_H, Math.round(window.innerHeight * 0.8));
const clampH = (h: number) => Math.min(Math.max(h, MIN_H), maxHeight());

interface Props {
  eid: string;
  sid: string;
  replica: string;
  /** Types a path into the terminal. */
  onPaste: (text: string) => void;
}

export default function FilesDrawer({ eid, sid, replica, onPaste }: Props) {
  const [open, setOpen] = useState(() => readPref(DRAWER_KEY, true));
  const [height, setHeight] = useState(() => clampH(readPref(DRAWER_HEIGHT_KEY, DRAWER_DEFAULT_HEIGHT)));
  const [path, setPath] = useState("");
  const [data, setData] = useState<Listing | null>(null);
  const [err, setErr] = useState<unknown>(null);
  const [loading, setLoading] = useState(false);
  const [tick, setTick] = useState(0);
  const [up, setUp] = useState<{ name: string; pct: number } | null>(null);
  const [drag, setDrag] = useState(false);
  const [menu, setMenu] = useState<{ x: number; y: number; entry: FileEntry } | null>(null);
  const [doomed, setDoomed] = useState<FileEntry | null>(null);
  const fileInput = useRef<HTMLInputElement>(null);

  useEffect(() => { writePref(DRAWER_KEY, open); }, [open]);

  // The listing follows the open state, the folder, the instance and refresh.
  useEffect(() => {
    if (!open) return;
    let live = true;
    setLoading(true);
    setErr(null);
    listFiles(eid, sid, replica, path)
      .then((d) => { if (live) setData(d); })
      .catch((e) => { if (live) { setData(null); setErr(e); } })
      .finally(() => { if (live) setLoading(false); });
    return () => { live = false; };
  }, [open, eid, sid, replica, path, tick]);

  const crumbs = path ? path.split("/") : [];
  const rootName = data ? data.cwd.split("/").filter(Boolean).pop() ?? data.cwd : "";

  const upload = async (files: FileList | File[]) => {
    for (const f of Array.from(files)) {
      setUp({ name: f.name, pct: 0 });
      try {
        await uploadFile(eid, sid, replica, path, f, (pct) => setUp({ name: f.name, pct }));
      } catch (e) {
        toastError(e);
        break;
      }
    }
    setUp(null);
    setTick((n) => n + 1);
  };

  const onDrop = (e: DragEvent) => {
    e.preventDefault();
    setDrag(false);
    if (e.dataTransfer.files.length) void upload(e.dataTransfer.files);
  };

  const onHandleKey = (e: KeyboardEvent) => {
    if (e.key !== "ArrowUp" && e.key !== "ArrowDown") return;
    e.preventDefault();
    setHeight((h) => {
      const next = clampH(h + (e.key === "ArrowUp" ? 24 : -24));
      writePref(DRAWER_HEIGHT_KEY, next);
      return next;
    });
  };

  const startResize = (e: RPointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    const startY = e.clientY;
    const startH = height;
    let latest = startH;
    const move = (ev: PointerEvent) => {
      latest = clampH(startH + (startY - ev.clientY));
      setHeight(latest);
    };
    const up2 = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up2);
      writePref(DRAWER_HEIGHT_KEY, latest);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up2);
  };

  if (!open) {
    return (
      <button type="button" onClick={() => setOpen(true)} aria-expanded={false}
        className="flex h-7 w-full shrink-0 items-center justify-center gap-1.5 border-t border-white/10 text-caption text-fg-3 transition-colors duration-150 hover:text-fg-2">
        <Folder size={12} />
        Files
        <ChevronUp size={12} />
      </button>
    );
  }

  return (
    <section aria-label="Files" className="relative flex shrink-0 flex-col border-t border-white/10" style={{ height }}>
      <div role="separator" aria-label="Resize files" aria-orientation="horizontal" tabIndex={0}
        onPointerDown={startResize} onKeyDown={onHandleKey}
        className="flex h-1.5 shrink-0 cursor-row-resize touch-none items-center justify-center outline-none focus-visible:shadow-none focus-visible:bg-white/5">
        <span className="console-grip h-0.5 w-8 rounded" />
      </div>

      <header className="flex h-7 shrink-0 items-center gap-3 px-4 text-caption text-fg-5 sm:px-12">
        <button type="button" onClick={() => setPath("")} aria-label="Go to the working directory"
          className="flex h-5 w-5 items-center justify-center rounded text-fg-4 transition-colors duration-150 hover:text-fg-2">
          <HardDrive size={12} />
        </button>
        <nav aria-label="Folder path" className="console-mono flex min-w-0 items-center gap-1 overflow-hidden whitespace-nowrap text-caption">
          {data && (
            <>
              <ChevronRight size={10} className="shrink-0 text-fg-5" />
              <button type="button" onClick={() => setPath("")} className={cx("truncate transition-colors duration-150 hover:text-foreground", crumbs.length ? "text-fg-3" : "text-fg-2")}>{rootName}</button>
            </>
          )}
          {crumbs.map((c, i) => (
            <span key={`${i}-${c}`} className="flex items-center gap-1">
              <ChevronRight size={10} className="shrink-0 text-fg-5" />
              <button type="button" onClick={() => setPath(crumbs.slice(0, i + 1).join("/"))}
                className={cx("truncate transition-colors duration-150 hover:text-foreground", i === crumbs.length - 1 ? "text-fg-2" : "text-fg-3")}>{c}</button>
            </span>
          ))}
        </nav>
        <span className="flex-1" />
        {up ? (
          <span className="console-mono truncate text-caption text-fg-3" aria-live="polite">Uploading {up.name} {up.pct}%</span>
        ) : (
          <button type="button" onClick={() => fileInput.current?.click()} disabled={!data}
            className="flex items-center gap-1.5 text-caption text-fg-3 transition-colors duration-150 hover:text-foreground disabled:opacity-40">
            <Upload size={12} />
            Upload
          </button>
        )}
        <input ref={fileInput} type="file" multiple className="hidden" aria-label="Upload files"
          onChange={(e) => { if (e.target.files?.length) void upload(e.target.files); e.target.value = ""; }} />
        <Tooltip content="Reload this folder">
          <button type="button" onClick={() => setTick((n) => n + 1)} aria-label="Reload this folder"
            className="flex h-5 w-5 items-center justify-center rounded text-fg-4 transition-colors duration-150 hover:text-fg-2">
            <RefreshCw size={12} className={cx(loading && "animate-spin")} />
          </button>
        </Tooltip>
        <button type="button" onClick={() => setOpen(false)} aria-label="Collapse files" aria-expanded
          className="flex h-5 w-5 items-center justify-center rounded text-fg-4 transition-colors duration-150 hover:text-fg-2">
          <ChevronDown size={12} />
        </button>
      </header>

      <div
        className={cx("scroll-thin relative min-h-0 flex-1 overflow-y-auto transition-colors duration-150", drag && "bg-white/[0.03] outline outline-1 -outline-offset-4 outline-dashed outline-accent-text")}
        onDragOver={(e) => { if (e.dataTransfer.types.includes("Files")) { e.preventDefault(); setDrag(true); } }}
        onDragLeave={(e) => { if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setDrag(false); }}
        onDrop={onDrop}
      >
        {err ? (
          <ListNotice tone="danger" action={<button type="button" onClick={() => setTick((n) => n + 1)} className="text-caption text-foreground underline underline-offset-2">Try again</button>}>
            {listErrorText(err)}
          </ListNotice>
        ) : !data ? (
          <ListNotice icon={<Spinner size={14} />}>Loading...</ListNotice>
        ) : data.entries.length === 0 ? (
          <ListNotice action={<button type="button" onClick={() => fileInput.current?.click()} className="text-caption text-foreground underline underline-offset-2">Upload a file</button>}>
            This folder is empty. Drop files here to upload them.
          </ListNotice>
        ) : (
          <ul aria-label="Files in this folder" className="console-mono py-0.5 text-caption leading-[18px]">
            {sortEntries(data.entries).map((e) => (
              <li key={e.name}>
                <FileRow entry={e}
                  onOpen={() => { if (e.type === "dir") setPath(joinPath(path, e.name)); }}
                  onMenu={(x, y) => setMenu({ x, y, entry: e })} />
              </li>
            ))}
          </ul>
        )}
      </div>

      {menu && data && (
        <RowMenu x={menu.x} y={menu.y} entry={menu.entry}
          onClose={() => setMenu(null)}
          onPaste={() => {
            onPaste(shellQuote(absPath(data.cwd, joinPath(path, menu.entry.name))));
            setMenu(null);
          }}
          onDownload={() => {
            setMenu(null);
            if (menu.entry.type === "dir") { toast("Folders download from the terminal with tar.", "info"); return; }
            const a = document.createElement("a");
            a.href = downloadHref(eid, sid, replica, joinPath(path, menu.entry.name));
            a.download = menu.entry.name;
            document.body.append(a);
            a.click();
            a.remove();
          }}
          onDelete={() => { setDoomed(menu.entry); setMenu(null); }} />
      )}

      {doomed && (
        <Confirm
          title={`Delete ${doomed.name}?`}
          body={doomed.type === "dir" ? "The folder and everything inside it will be removed from this instance. This cannot be undone." : "The file will be removed from this instance. This cannot be undone."}
          confirm="Delete"
          danger
          onClose={() => setDoomed(null)}
          onConfirm={async () => {
            try {
              await deleteFile(eid, sid, replica, joinPath(path, doomed.name));
              setDoomed(null);
              setTick((n) => n + 1);
              toast(`Deleted ${doomed.name}.`, "success");
            } catch (e) {
              toastError(e);
            }
          }}
        />
      )}
    </section>
  );
}

function FileRow({ entry, onOpen, onMenu }: { entry: FileEntry; onOpen: () => void; onMenu: (x: number, y: number) => void }) {
  const badge = fileBadge(entry);
  const isDir = entry.type === "dir";
  return (
    <div
      role={isDir ? "button" : undefined}
      tabIndex={isDir ? 0 : undefined}
      onClick={onOpen}
      onKeyDown={(e) => { if (isDir && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); onOpen(); } }}
      onContextMenu={(e) => { e.preventDefault(); onMenu(e.clientX, e.clientY); }}
      className={cx("flex h-7 items-center px-4 transition-colors duration-100 hover:bg-white/5 focus-visible:bg-white/5 sm:px-12", isDir && "cursor-pointer")}
    >
      <span className="flex w-5 shrink-0 items-center justify-center">
        {badge.kind === "folder" && <Folder size={14} fill="currentColor" strokeWidth={1.5} className="file-badge-dir" />}
        {badge.kind === "text" && <span className={cx("text-[10px] font-bold leading-none", badge.cls)}>{badge.text}</span>}
        {badge.kind === "page" && <FileText size={12} className="text-fg-4" />}
      </span>
      <span className={cx("ml-2 min-w-0 flex-1 truncate", isDir ? "text-gray-700" : "text-fg-2")}>
        {entry.name}{isDir ? "/" : ""}
      </span>
      <span className="ml-3 w-20 shrink-0 text-right text-fg-4">{isDir ? "" : fmtBytes(entry.size)}</span>
      <span className="ml-3 hidden w-36 shrink-0 text-right text-fg-4 md:block">{formatStamp(entry.modified)}</span>
    </div>
  );
}

function RowMenu({ x, y, entry, onClose, onPaste, onDownload, onDelete }: {
  x: number; y: number; entry: FileEntry; onClose: () => void; onPaste: () => void; onDownload: () => void; onDelete: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  useClickOutside(ref, onClose, true);
  useEffect(() => {
    const key = (e: globalThis.KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    const scroll = () => onClose();
    window.addEventListener("keydown", key);
    window.addEventListener("resize", scroll);
    return () => { window.removeEventListener("keydown", key); window.removeEventListener("resize", scroll); };
  }, [onClose]);
  const left = Math.min(x, window.innerWidth - 176);
  const top = Math.min(y, window.innerHeight - 120);
  return (
    <div ref={ref} role="menu" aria-label={`Actions for ${entry.name}`} style={{ left, top }}
      className="animate-pop-in fixed z-50 w-40 rounded-menu border border-white/10 bg-gray-200 p-1 shadow-popover">
      <MenuRow icon={<ClipboardPaste size={12} />} onClick={onPaste}>Paste filepath</MenuRow>
      {entry.type === "file" && <MenuRow icon={<Download size={12} />} onClick={onDownload}>Download</MenuRow>}
      <MenuRow icon={<Trash2 size={12} />} onClick={onDelete} danger>Delete</MenuRow>
    </div>
  );
}

function MenuRow({ icon, children, onClick, danger }: { icon: ReactNode; children: ReactNode; onClick: () => void; danger?: boolean }) {
  return (
    <button type="button" role="menuitem" onClick={onClick}
      className={cx("flex h-[30px] w-full items-center gap-2 rounded px-3 text-caption transition-colors duration-100 hover:bg-white/10", danger ? "text-danger-text" : "text-foreground")}>
      {icon}
      {children}
    </button>
  );
}

function ListNotice({ children, icon, action, tone }: { children: ReactNode; icon?: ReactNode; action?: ReactNode; tone?: "danger" }) {
  return (
    <div className={cx("console-mono flex h-full min-h-[96px] flex-col items-center justify-center gap-2 px-4 text-center text-caption", tone === "danger" ? "text-danger-soft" : "text-fg-4")} role={tone === "danger" ? "alert" : "status"}>
      <span className="flex items-center gap-2">{icon}{children}</span>
      {action}
    </div>
  );
}

function listErrorText(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === "path_not_found") return "That folder no longer exists.";
    if (e.status === 404 || e.status === 501) return "File browsing is not available on this server yet.";
  }
  return `Could not load this folder. ${errText(e)}`;
}

/** Quotes a path for the shell when it has characters the shell would split on. */
function shellQuote(p: string): string {
  return /^[\w@%+=:,./-]+$/.test(p) ? p : `'${p.replace(/'/g, `'\\''`)}'`;
}
