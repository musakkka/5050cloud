// SPDX-License-Identifier: AGPL-3.0-only

// The bell: in-app notifications (9.24). Reloads every 60 s and on open.
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Bell as BellIcon } from "lucide-react";
import { api, type Notification } from "../../api/client";
import { Popover, RelTime, cx, optimistic } from "../../kit";

export function Bell() {
  const [items, setItems] = useState<Notification[]>([]);
  const nav = useNavigate();
  const load = () => api.notifications().then((p) => setItems(p.items || []), () => {});
  useEffect(() => { void load(); const t = setInterval(load, 60000); return () => clearInterval(t); }, []);
  const unread = items.filter((n) => !n.readAt).length;
  const markAll = () => {
    const before = items;
    void optimistic(() => setItems(items.map((n) => ({ ...n, readAt: n.readAt || Date.now() / 1000 }))), () => api.readNotifications("all"), () => setItems(before));
  };
  return (
    <Popover align="right" minWidth={320} trigger={() => (
      <button type="button" aria-label={unread ? `Notifications, ${unread} unread` : "Notifications"} onClick={() => void load()}
        className="relative flex h-8 w-8 items-center justify-center rounded-md text-gray-600 hover:bg-gray-100 hover:text-foreground">
        <BellIcon size={17} />
        {unread > 0 && <span className="absolute right-1 top-1 h-2 w-2 rounded-full bg-red-500" />}
      </button>
    )}>
      {(close) => (
        <div className="w-[min(360px,calc(100vw-32px))]">
          <div className="flex items-center justify-between px-3 pb-2 pt-1">
            <span className="text-sm font-medium">Notifications</span>
            {unread > 0 && <button type="button" className="text-xs text-gray-500 hover:text-foreground" onClick={markAll}>Mark all read</button>}
          </div>
          {items.length === 0 && <div className="px-3 py-6 text-center text-sm text-gray-500">Nothing new.</div>}
          {items.map((n) => (
            <button type="button" key={n.id} onClick={() => { close(); if (!n.readAt) void api.readNotifications([n.id]).catch(() => {}); if (n.link) nav(n.link); }}
              className={cx("block w-full px-3 py-2 text-left text-sm hover:bg-gray-100", !n.readAt && "font-medium")}>
              <div className="text-gray-800">{n.text}</div>
              <div className="text-xs text-gray-500"><RelTime ts={n.ts} /></div>
            </button>
          ))}
        </div>
      )}
    </Popover>
  );
}
