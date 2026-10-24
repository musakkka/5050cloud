// SPDX-License-Identifier: AGPL-3.0-only

// Dashboard project card (grid) and row (list). The whole card is one link to the project; the
// favourite star and the service tiles sit above it as their own controls (tiles open the service).
import { memo } from "react";
import { Link } from "react-router-dom";
import { Star } from "lucide-react";
import type { Project } from "../../api/client";
import { ServiceIcon, Skeleton, StatusDot, cx, toneOf } from "../../kit";
import { onlineText, primaryEnv, primarySummary, summaryTone, tileLayout, tilesOf, type Tile } from "./model";

/** The 6px dot, environment name, a middle dot and the online count, in 12px secondary text. */
function StatusLine({ p, className }: { p: Project; className?: string }) {
  const s = primarySummary(p);
  const env = primaryEnv(p);
  return (
    <div className={cx("flex min-w-0 items-center gap-2 text-caption text-fg-2", className)}>
      <StatusDot tone={summaryTone(s)} size={6} pulse={false} />
      {env && <span className="truncate">{env.name}</span>}
      {env && <span aria-hidden>·</span>}
      <span className="whitespace-nowrap">{onlineText(s)}</span>
    </div>
  );
}

export function FavouriteButton({ on, name, onToggle }: { on: boolean; name: string; onToggle: () => void }) {
  return (
    <button type="button" aria-pressed={on} aria-label={on ? `Remove ${name} from favourites` : `Add ${name} to favourites`}
      title={on ? "Remove from favourites" : "Add to favourites"}
      onClick={(e) => { e.preventDefault(); e.stopPropagation(); onToggle(); }}
      className={cx("pointer-events-auto relative z-20 inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md transition-colors duration-150 active:scale-90",
        on ? "text-yellow-500 hover:text-yellow-600" : "text-fg-4 hover:text-foreground")}>
      <Star size={16} fill={on ? "currentColor" : "none"} className={cx("transition-transform duration-150", on && "scale-105")} />
    </button>
  );
}

function ServiceTile({ pid, t }: { pid: string; t: Tile }) {
  const bad = t.status ? toneOf(t.status) === "bad" : false;
  const tile = (
    <span className="relative flex h-10 w-10 items-center justify-center rounded-lg border border-line-popover bg-surface-hover transition-colors duration-150 group-hover/tile:border-line-panel group-hover/tile:bg-gray-200">
      <ServiceIcon icon={t.icon} source={t.source} size={20} />
      {bad && <span aria-label="Crashed" role="img" className="absolute -right-0.5 -top-0.5 h-2 w-2 rounded-full bg-red-500 ring-2 ring-background" />}
    </span>
  );
  if (!t.serviceId) return <span className="group/tile">{tile}</span>;
  return (
    <Link to={`/project/${encodeURIComponent(pid)}/service/${encodeURIComponent(t.serviceId)}`} aria-label="Open service"
      className="group/tile pointer-events-auto relative z-20 rounded-xl focus:outline-none focus-visible:shadow-ring">
      {tile}
    </Link>
  );
}

/** The canvas well: a dotted 10px grid with the services as a centred block of 40px tiles. */
function Well({ p }: { p: Project }) {
  const tiles = tilesOf(primarySummary(p));
  const { shown, more, cols } = tileLayout(tiles.length);
  return (
    <div className="relative flex min-h-[220px] flex-1 items-center justify-center rounded bg-background py-6 shadow-well transition-colors duration-150 group-hover/card:bg-surface-raised"
      style={{
        backgroundImage: "radial-gradient(circle, color-mix(in srgb, var(--foreground) 8%, transparent) 1px, transparent 1px)",
        backgroundSize: "10px 10px",
        backgroundPosition: "calc(50% - 5px) calc(50% - 10px)",
      }}>
      {tiles.length > 0 && (
        <div className="-translate-y-2.5">
          <div className="grid w-fit gap-[10px]" style={{ gridTemplateColumns: `repeat(${cols}, 40px)` }}>
            {tiles.slice(0, shown).map((t) => <ServiceTile key={t.key} pid={p.id} t={t} />)}
            {more > 0 && (
              <span className="flex h-10 w-10 items-center justify-center rounded-lg border border-line-popover bg-surface-hover text-caption font-medium text-fg-2" title={`${more} more services`}>
                +{more}
              </span>
            )}
          </div>
        </div>
      )}
      <StatusLine p={p} className="absolute inset-x-0 bottom-0 p-2" />
    </div>
  );
}

export const ProjectCard = memo(function ProjectCard({ p, favourite, onFavourite }: { p: Project; favourite: boolean; onFavourite: (id: string) => void }) {
  return (
    <li className="h-full" data-project-id={p.id}>
      <div className="group/card relative h-full overflow-hidden rounded-lg bg-surface shadow-card outline outline-1 outline-line-popover transition-[background-color,outline-color,box-shadow] duration-150 hover:bg-surface-hover hover:outline-line-panel focus-within:outline-line-panel">
        <Link to={`/project/${encodeURIComponent(p.id)}`} className="absolute inset-0 z-0 rounded-lg focus:outline-none focus-visible:shadow-ring">
          <span className="sr-only">Open {p.name}</span>
        </Link>
        <div className="pointer-events-none relative z-10 flex h-full flex-col">
          <div className="flex items-center gap-1.5 p-4">
            <span className="min-w-0 flex-1 truncate text-sm font-semibold text-foreground" title={p.name}>{p.name}</span>
            <FavouriteButton on={favourite} name={p.name} onToggle={() => onFavourite(p.id)} />
          </div>
          <div className="flex min-h-0 flex-1 flex-col px-2 pb-2">
            <Well p={p} />
          </div>
        </div>
      </div>
    </li>
  );
});

export const ProjectRow = memo(function ProjectRow({ p, favourite, onFavourite }: { p: Project; favourite: boolean; onFavourite: (id: string) => void }) {
  return (
    <li data-project-id={p.id}>
      <div className="relative overflow-hidden rounded-xl bg-surface p-4 outline outline-1 outline-line-popover transition-[background-color,outline-color] duration-150 hover:bg-surface-hover hover:outline-line-panel focus-within:outline-line-panel">
        <Link to={`/project/${encodeURIComponent(p.id)}`} className="absolute inset-0 z-0 rounded-xl focus:outline-none focus-visible:shadow-ring">
          <span className="sr-only">Open {p.name}</span>
        </Link>
        <div className="pointer-events-none relative z-10 flex flex-col justify-between gap-2 md:flex-row md:items-center">
          <p className="min-w-0 truncate text-sm font-medium text-foreground" title={p.name}>{p.name}</p>
          <div className="flex items-center justify-between gap-4 md:justify-end">
            <StatusLine p={p} className="gap-1.5" />
            <FavouriteButton on={favourite} name={p.name} onToggle={() => onFavourite(p.id)} />
          </div>
        </div>
      </div>
    </li>
  );
});

/** Placeholders in the exact card and row frames, so the page does not shift when data lands. */
export function CardSkeleton() {
  return (
    <li aria-hidden className="h-[288px] overflow-hidden rounded-lg bg-surface outline outline-1 outline-line-popover">
      <div className="flex h-[60px] items-center justify-between p-4">
        <Skeleton w={120} h={16} />
        <Skeleton w={16} h={16} />
      </div>
      <div className="px-2 pb-2">
        <div className="dot-grid relative flex h-[220px] items-center justify-center rounded bg-background shadow-well">
          <Skeleton w={40} h={40} className="-translate-y-2.5 !rounded-lg" />
          <div className="absolute bottom-2 left-2 flex items-center gap-2"><Skeleton circle w={6} h={6} /><Skeleton w={150} h={12} /></div>
        </div>
      </div>
    </li>
  );
}

export function RowSkeleton() {
  return (
    <li aria-hidden className="flex h-14 items-center justify-between rounded-xl bg-surface px-4 outline outline-1 outline-line-popover">
      <Skeleton w={140} h={16} />
      <div className="flex items-center gap-4"><Skeleton w={170} h={12} /><Skeleton w={16} h={16} /></div>
    </li>
  );
}
