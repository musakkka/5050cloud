// SPDX-License-Identifier: AGPL-3.0-only

import { AlertTriangle, Mail, OctagonAlert } from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import type * as T from "../../api/types.gen";
import { Notice, Select, Skeleton, cx, toastError } from "../../kit";
import { v1 } from "../settings/client";
import { Loading, Section, optimistic, useLoad } from "../settings/ui";
import { NOTIFY_EVENTS, fromDelivery, fullPrefs, toDelivery, type Delivery, type Severity } from "./notify";

const DELIVERY: { value: Delivery; label: string }[] = [
  { value: "both", label: "Email & In-App" },
  { value: "email", label: "Email Only" },
  { value: "app", label: "In-App Only" },
  { value: "none", label: "None" },
];
const CHIP: Record<Severity, string> = { danger: "bg-red-100 text-danger-text", warn: "bg-yellow-50 text-warn", neutral: "bg-surface-hover text-fg-2" };

export default function NotificationsPage({ smtp }: { smtp: boolean }) {
  const { data, error } = useLoad(v1.notificationPrefs, []);
  const [prefs, setPrefs] = useState<T.NotificationPref[] | null>(null);
  useEffect(() => { if (data) setPrefs(fullPrefs(data)); }, [data]);
  if (!prefs) return <Loading error={error} />;
  const setOne = (event: string, d: Delivery) => {
    const next = prefs.map((p) => (p.event === event ? { ...p, ...fromDelivery(d) } : p));
    void optimistic(prefs, next, setPrefs, () => v1.setNotificationPrefs(next), (m) => toastError(new Error(m)));
  };
  return (
    <>
      {!smtp && <Notice tone="warn" icon={<Mail size={16} />}>Email is not set up on this install, so only in-app notifications are sent. <Link to="/settings/general" className="underline">Set up email</Link></Notice>}
      <Section title="Notification Rules" description="Choose how you hear about each event. Changes save as you make them.">
        <div className="overflow-hidden rounded-lg border border-gray-100">
          {NOTIFY_EVENTS.map((e) => {
            const p = prefs.find((x) => x.event === e.id)!;
            const Icon = e.tone === "danger" ? OctagonAlert : AlertTriangle;
            return (
              <div key={e.id} className="flex flex-col gap-3 border-b border-gray-100 p-4 last:border-b-0 sm:flex-row sm:items-center sm:justify-between">
                <div className="min-w-0 space-y-1.5">
                  <div className="text-body font-medium">All Projects</div>
                  <div className="flex flex-wrap items-center gap-2">
                    <span className={cx("inline-flex h-8 items-center gap-2 rounded px-3 text-body", CHIP[e.tone])}><Icon size={16} />{e.label}</span>
                    <span className="text-caption text-fg-3">{e.help}</span>
                  </div>
                </div>
                <Select variant="field" className="sm:!w-[168px]" aria-label={`Delivery for ${e.label}`} value={toDelivery(p)} onChange={(ev) => setOne(e.id, ev.target.value as Delivery)}
                  options={DELIVERY.map((d) => ({ ...d, disabled: !smtp && d.value !== "app" && d.value !== "none" }))} />
              </div>
            );
          })}
        </div>
      </Section>
    </>
  );
}

export function NotificationsSkeleton() { return <Skeleton h={300} />; }
