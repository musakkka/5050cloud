// SPDX-License-Identifier: AGPL-3.0-only

// Service Settings (docs/SPEC.md 13.3): one long page of sections on a
// timeline with a right-hand index that follows the scroll, and a filter
// (press /) that hides fields and sections. Every field stages a merge patch
// of the service config; fields set by a config file are read-only.

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { Box, Cloud, Code2, FileText, Flag, Flame, Globe, Network, X, Zap } from "lucide-react";
import type { Service } from "../../api/types.gen";
import { patch, svcPath } from "../service/client";
import { ErrorLine, Skeleton, cx, toastError } from "../../kit";
import { Settings, isDatabase, type SettingsCtx } from "./context";
import { patchFor } from "./patch";
import { FilterProvider, SettingsSection, Timeline, type SectionDef } from "./ui";
import SourceSection from "./SourceSection";
import Networking from "./Networking";
import EdgeSection from "./EdgeSection";
import ScaleSection from "./ScaleSection";
import BuildSection from "./BuildSection";
import DeploySection from "./DeploySection";
import { ConfigSection, DangerSection, FlagsSection } from "./MiscSections";

// `npm run dev:mocks`: stateful settings fixtures (mocks.ts), compiled out of production builds.

const ALL: SectionDef[] = [
  { id: "source", label: "Source", icon: <Code2 /> },
  { id: "networking", label: "Networking", icon: <Network /> },
  { id: "edge", label: "Edge", icon: <Zap /> },
  { id: "scale", label: "Scale", icon: <Globe /> },
  { id: "build", label: "Build", icon: <Box /> },
  { id: "deploy", label: "Deploy", icon: <Cloud /> },
  { id: "config", label: "Config-as-code", icon: <FileText /> },
  { id: "flags", label: "Feature-flags", icon: <Flag /> },
  { id: "danger", label: "Danger", icon: <Flame />, danger: true },
];

/** Sections shown for a service: images and databases have no Build, Edge or Config-as-code. */
export function sectionsFor(s: Service): SectionDef[] {
  const repo = (s.staged ?? s.config).source.type === "github";
  const db = isDatabase(s);
  return ALL.filter((d) => {
    if (d.id === "build" || d.id === "config") return repo;
    if (d.id === "edge") return !db && s.kind !== "cron" && s.kind !== "worker";
    return true;
  });
}

export default function SettingsTab({ eid, service, onService, onDeleted }: {
  eid: string; service: Service; onService: (s: Service) => void; onDeleted?: () => void;
}) {
  const base = svcPath(eid, service.id);
  const [filter, setFilter] = useState("");
  const [err, setErr] = useState<unknown>(null);
  const filterRef = useRef<HTMLInputElement>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  const q = filter.trim().toLowerCase();
  const sections = useMemo(() => sectionsFor(service), [service]);
  const [empty, setEmpty] = useState<Set<string>>(new Set());
  const [active, setActive] = useState(sections[0]?.id);

  // "/" focuses the filter from anywhere on the page; Escape in it clears.
  useEffect(() => {
    const k = (e: KeyboardEvent) => {
      const t = e.target as HTMLElement;
      if (e.key === "/" && !e.metaKey && !e.ctrlKey && !["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName) && !t.isContentEditable) {
        e.preventDefault();
        filterRef.current?.focus();
      }
    };
    window.addEventListener("keydown", k);
    return () => window.removeEventListener("keydown", k);
  }, []);

  // Sections left with no fields by the filter are hidden, along with their index entry.
  useLayoutEffect(() => {
    const root = rootRef.current;
    if (!root) return;
    const out = new Set<string>();
    if (q) root.querySelectorAll<HTMLElement>("[data-section]").forEach((el) => { if (!el.querySelector("[data-field]")) out.add(el.dataset.section!); });
    setEmpty((prev) => (prev.size === out.size && [...out].every((x) => prev.has(x)) ? prev : out));
  }, [q, service]);

  // Scroll spy: the last section whose heading is above the top third of the scroller.
  const scroller = useCallback(() => {
    let el = rootRef.current?.parentElement ?? null;
    while (el && !/(auto|scroll)/.test(getComputedStyle(el).overflowY)) el = el.parentElement;
    return el;
  }, []);
  useEffect(() => {
    const sc = scroller();
    if (!sc) return;
    const spy = () => {
      const top = sc.getBoundingClientRect().top + 140;
      let cur = sections[0]?.id;
      for (const s of sections) {
        const el = document.getElementById(`settings-${s.id}`);
        if (el && !el.hidden && el.getBoundingClientRect().top <= top) cur = s.id;
      }
      if (sc.scrollTop + sc.clientHeight >= sc.scrollHeight - 4) cur = [...sections].reverse().find((s) => !empty.has(s.id))?.id ?? cur;
      setActive(cur);
    };
    spy();
    sc.addEventListener("scroll", spy, { passive: true });
    return () => sc.removeEventListener("scroll", spy);
  }, [scroller, sections, empty]);

  const stagePatch = useCallback(async (p: Record<string, unknown>) => {
    setErr(null);
    try { onService(await patch<Service>(`${base}/config`, { patch: p })); } catch (e) { setErr(e); toastError(e); throw e; }
  }, [base, onService]);
  const ctx: SettingsCtx = {
    eid, base, svc: service, cfg: service.staged ?? service.config, onService, onDeleted,
    stagePatch, stage: (path, value) => stagePatch(patchFor(path, value)),
  };
  const shown = sections.filter((s) => !empty.has(s.id));

  const body: Record<string, JSX.Element> = {
    source: <SourceSection />,
    networking: <Networking />,
    edge: <EdgeSection />,
    scale: <ScaleSection />,
    build: <BuildSection />,
    deploy: <DeploySection />,
    config: <ConfigSection />,
    flags: <FlagsSection />,
    danger: <DangerSection />,
  };

  return (
    <Settings.Provider value={ctx}>
      <div ref={rootRef} className="-mt-8">
        {/* Filter bar: stays under the tabs while the page scrolls. */}
        <div className="sticky top-[-32px] z-10 -mx-4 border-b border-line bg-secondaryBg px-4 pb-3 pt-3 sm:-mx-12 sm:px-12">
          <div className="relative">
            <input ref={filterRef} aria-label="Filter settings" placeholder="Filter Settings..." value={filter}
              onChange={(e) => setFilter(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Escape") { e.stopPropagation(); setFilter(""); filterRef.current?.blur(); } }}
              className="h-[34px] w-full rounded-md border border-line bg-secondaryBg pl-1.5 pr-10 text-base text-foreground outline-none transition-colors duration-150 placeholder:text-fg-2 hover:border-fg-5 focus:border-accent-muted focus:shadow-none" />
            {filter ? (
              <button type="button" aria-label="Clear filter" onClick={() => { setFilter(""); filterRef.current?.focus(); }}
                className="absolute right-[3px] top-[3px] inline-flex h-7 w-7 items-center justify-center rounded border border-surface-hover bg-background text-fg-2 transition-colors hover:text-foreground">
                <X size={13} />
              </button>
            ) : (
              <kbd aria-hidden className="pointer-events-none absolute right-[3px] top-[3px] inline-flex h-7 w-7 items-center justify-center rounded border border-surface-hover bg-background font-sans text-sm text-fg-2">/</kbd>
            )}
          </div>
        </div>

        <div className="flex gap-10 pt-10 sm:-ml-3">
          <div className="min-w-0 max-w-[600px] flex-1">
            {service.configFile && !q && (
              <div className="mb-8 ml-[62px] max-w-[536px] rounded-md bg-surface-hover px-4 py-3 text-sm text-fg-2">
                Fields marked with a file tag come from{" "}
                {service.configFile.url ? <a className="text-foreground underline underline-offset-2" href={service.configFile.url} target="_blank" rel="noreferrer">{service.configFile.path}</a> : <span className="font-mono text-foreground">{service.configFile.path}</span>}
                {" "}and are read-only here.
              </div>
            )}
            <ErrorLine error={err} className="mb-6 ml-[62px] max-w-[536px]" />
            <FilterProvider value={{ q, sectionMatch: false }}>
              <Timeline>
                {sections.map((s) => <SettingsSection key={s.id} def={s} hidden={empty.has(s.id)}>{body[s.id]}</SettingsSection>)}
              </Timeline>
            </FilterProvider>
            {q && shown.length === 0 && (
              <div className="py-16 text-center text-sm text-fg-3">No settings match <span className="text-foreground">"{filter}"</span>.</div>
            )}
          </div>
          <nav aria-label="Settings sections" className="sticky top-[70px] ml-auto hidden h-fit w-[120px] shrink-0 lg:block">
            <ul className="flex flex-col gap-3">
              {shown.map((s) => (
                <li key={s.id}>
                  <a href={`#settings-${s.id}`} aria-current={active === s.id ? "location" : undefined}
                    className={cx("block text-sm transition-colors duration-150 hover:text-foreground", active === s.id && !q ? "text-foreground" : "text-fg-3")}
                    onClick={(e) => { e.preventDefault(); document.getElementById(`settings-${s.id}`)?.scrollIntoView({ behavior: "smooth", block: "start" }); }}>
                    {s.label}
                  </a>
                </li>
              ))}
            </ul>
          </nav>
        </div>
      </div>
    </Settings.Provider>
  );
}

/** The settings page while the service loads: filter bar and three section outlines. */
export function SettingsSkeleton() {
  return (
    <div aria-busy className="flex flex-col gap-16 pt-10">
      {[0, 1, 2].map((i) => (
        <div key={i} className="flex gap-[22px]">
          <Skeleton circle w={40} h={40} />
          <div className="flex flex-1 flex-col gap-3 pt-2">
            <Skeleton w={140} h={20} />
            <Skeleton w={260} h={14} className="mt-4" />
            <Skeleton w={420} h={14} />
            <Skeleton w={536} h={44} className="max-w-full" />
          </div>
        </div>
      ))}
    </div>
  );
}
