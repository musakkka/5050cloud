// SPDX-License-Identifier: AGPL-3.0-only

import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { api, onUnauthorized, type Me } from "../api/client";
import { Spinner } from "../kit";
import { ShellSkeleton } from "./Shell";
import { AuthGate } from "../features/auth/AuthPages";

interface Session { me: Me | null; ready: boolean; setMe: (m: Me | null) => void; refresh: () => Promise<void> }
const Ctx = createContext<Session>({ me: null, ready: false, setMe: () => {}, refresh: async () => {} });

export function SessionProvider({ children }: { children: ReactNode }) {
  const [me, setMe] = useState<Me | null>(null);
  const [ready, setReady] = useState(false);
  const refresh = useCallback(async () => {
    try { setMe(await api.me()); } catch { setMe(null); } finally { setReady(true); }
  }, []);
  useEffect(() => { void refresh(); }, [refresh]);
  useEffect(() => onUnauthorized(() => setMe(null)), []);
  return <Ctx.Provider value={{ me, ready, setMe, refresh }}>{children}</Ctx.Provider>;
}

export const useSession = () => useContext(Ctx);

/** The signed-in user. Only call it below <RequireAuth>. */
export function useMe(): Me {
  const { me } = useSession();
  if (!me) throw new Error("useMe outside RequireAuth");
  return me;
}

export const canEdit = (me: Me) => me.user.role !== "viewer";
export const isAdmin = (me: Me) => me.user.role === "admin" || me.user.role === "owner";

/** Sends visitors to /setup on a fresh install and to /login otherwise. */
export function RequireAuth({ children }: { children: ReactNode }) {
  const { me, ready } = useSession();
  const loc = useLocation();
  const [needsSetup, setNeedsSetup] = useState<boolean | null>(null);
  useEffect(() => {
    if (ready && !me) api.setupStatus().then((s) => setNeedsSetup(s.needsSetup), () => setNeedsSetup(false));
  }, [ready, me]);
  if (!ready) return <ShellSkeleton />;
  if (me) return <>{children}</>;
  if (needsSetup === null) return <Center><Spinner /></Center>;
  if (needsSetup) return <Navigate to={`/setup${loc.search}`} replace />;
  return <AuthGate />;
}

export function Center({ children }: { children: ReactNode }) {
  return <div className="flex min-h-full items-center justify-center p-4">{children}</div>;
}
