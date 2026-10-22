// SPDX-License-Identifier: AGPL-3.0-only

import { type ReactNode } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api } from "../api/client";
import { Logo, Popover, MenuItem, toggleTheme, resolvedTheme, toastError } from "../kit";
import { Bell } from "../features/activity/Bell";
import { Palette, openPalette } from "../features/palette/Palette";
import { Search } from "lucide-react";
import { useMe, useSession } from "./session";

/** The top bar on every signed-in page. `left` holds breadcrumbs, `right` extra buttons. */
export function TopBar({ left, right }: { left?: ReactNode; right?: ReactNode }) {
  return (
    <header className="flex h-14 shrink-0 items-center gap-3 border-b border-gray-200 bg-background px-4">
      <Link to="/" aria-label="Dashboard" className="flex shrink-0 items-center text-foreground"><Logo size={26} /></Link>
      <div className="flex min-w-0 flex-1 items-center gap-2 text-sm">{left}</div>
      <div className="flex shrink-0 items-center gap-1">
        {right}
        <button type="button" aria-label="Command palette (Cmd+K)" title="Cmd+K" onClick={openPalette} className="flex h-8 w-8 items-center justify-center rounded-md text-gray-600 hover:bg-gray-100 hover:text-foreground"><Search size={16} /></button>
        <Bell />
        <AvatarMenu />
      </div>
      <Palette />
    </header>
  );
}

function AvatarMenu() {
  const me = useMe();
  const { setMe } = useSession();
  const nav = useNavigate();
  const initial = (me.user.name || me.user.email || "?").slice(0, 1).toUpperCase();
  return (
    <Popover align="right" minWidth={220} trigger={() => (
      <button type="button" aria-label="Account menu" className="ml-1 flex h-8 w-8 items-center justify-center rounded-full bg-pink-500 text-sm font-semibold text-white">{initial}</button>
    )}>
      {(close) => (
        <>
          <div className="border-b border-gray-200 px-3 pb-2 pt-1 text-xs text-gray-500"><div className="truncate text-sm text-foreground">{me.user.name}</div>{me.user.email}</div>
          <MenuItem onClick={() => { close(); nav("/account/profile"); }}>Account</MenuItem>
          <MenuItem onClick={() => { close(); nav("/settings/general"); }}>Install settings</MenuItem>
          <MenuItem onClick={() => { toggleTheme(); close(); }}>{resolvedTheme() === "dark" ? "Light theme" : "Dark theme"}</MenuItem>
          <MenuItem onClick={() => { close(); api.logout().then(() => { setMe(null); nav("/login"); }, toastError); }}>Log out</MenuItem>
        </>
      )}
    </Popover>
  );
}
