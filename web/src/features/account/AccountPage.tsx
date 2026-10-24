// SPDX-License-Identifier: AGPL-3.0-only

import { Bell, Flag, KeyRound, Lock, User } from "lucide-react";
import { Navigate, useParams } from "react-router-dom";
import type * as T from "../../api/types.gen";
import { v1 } from "../settings/client";
import { Loading, Page, useLoad, type PageNavItem } from "../settings/ui";
import FeatureFlags from "./FeatureFlags";
import NotificationsPage from "./NotificationsPage";
import ProfilePage from "./ProfilePage";
import SecurityPage from "./SecurityPage";
import TokensPanel from "./TokensPanel";

const NAV: PageNavItem[] = [
  { to: "/account/profile", label: "Account", icon: User },
  { to: "/account/notifications", label: "Notifications", icon: Bell },
  { to: "/account/feature-flags", label: "Feature Flags", icon: Flag },
  { to: "/account/tokens", label: "Tokens", icon: KeyRound },
  { to: "/account/security", label: "Security", icon: Lock },
];

export { NOTIFY_EVENTS, fullPrefs } from "./notify";

const TITLES: Record<string, string> = { profile: "Account Settings" };

export default function AccountPage() {
  const { section = "profile" } = useParams();
  const me = useLoad(v1.me, []);
  if (!NAV.some((n) => n.to.endsWith(`/${section}`))) return <Navigate to="/account/profile" replace />;
  const setUser = (user: T.User) => me.setData({ ...me.data!, user });
  return (
    <Page title="Account" nav={NAV} heading={TITLES[section]}>
      {!me.data ? <Loading error={me.error} /> : (
        <>
          {section === "profile" && <ProfilePage user={me.data.user} onUser={setUser} />}
          {section === "notifications" && <NotificationsPage smtp={me.data.install.smtpReady} />}
          {section === "feature-flags" && <FeatureFlags />}
          {section === "tokens" && <TokensPanel />}
          {section === "security" && <SecurityPage user={me.data.user} onUser={setUser} requireTotp={me.data.install.requireTotp} />}
        </>
      )}
    </Page>
  );
}
