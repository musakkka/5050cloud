// SPDX-License-Identifier: AGPL-3.0-only

import { Link } from "react-router-dom";
import type { ReactNode } from "react";
import { MessageSquare } from "lucide-react";
import { BrandIcon, Button } from "../../kit";
import { SectionHead } from "./ui";

interface Card { icon: string | ReactNode; name: string; body: string; label: string; to: string }

export default function Integrations({ pid, query }: { pid: string; query: string }) {
  const cards: Card[] = [
    { icon: "github", name: "GitHub", body: "Deploy on every push and read private repositories with the GitHub App.", label: "Connect to GitHub", to: "/settings/github" },
    { icon: "docker", name: "Container registries", body: "Save credentials so services can pull private images from Docker Hub, GHCR or any registry.", label: "Manage registries", to: "/settings/registry-credentials" },
    { icon: <MessageSquare size={40} strokeWidth={1.5} className="text-fg-2" />, name: "Slack and Discord", body: "Post deploys and alerts to a channel. Add the channel webhook URL and we format the message.", label: "Add a webhook", to: `/project/${pid}/settings/webhooks${query}` },
  ];
  return (
    <section>
      <SectionHead title="Integrations" description="Connect the tools your project already uses." />
      <div className="grid gap-4 sm:grid-cols-2">
        {cards.map((c) => (
          <div key={c.name} className="group flex min-h-[300px] flex-col items-center rounded-lg border border-line bg-background p-8 text-center transition-colors duration-150 hover:border-fg-5">
            <div className="flex h-12 w-12 items-center justify-center transition-transform duration-200 group-hover:scale-105">{typeof c.icon === "string" ? <BrandIcon name={c.icon} size={48} fallback="box" /> : c.icon}</div>
            <h3 className="mt-4 text-lead font-semibold text-foreground">{c.name}</h3>
            <p className="mt-2 max-w-[280px] text-body text-fg-2">{c.body}</p>
            <div className="mt-auto pt-6"><Link to={c.to} tabIndex={-1}><Button variant="accent-outline" size="lg">{c.label}</Button></Link></div>
          </div>
        ))}
      </div>
    </section>
  );
}
