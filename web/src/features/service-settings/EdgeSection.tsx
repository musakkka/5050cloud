// SPDX-License-Identifier: AGPL-3.0-only

// Settings > Edge: request shielding, edge caching and edge rules in front of
// the service's domains. v0.1 routes every request straight to the service,
// so these controls are shown disabled with what to do instead (SPEC 13.6).

import { useState } from "react";
import { ArrowDown, Shield } from "lucide-react";
import { Button, Modal, Tooltip } from "../../kit";
import { DropField, NotYetChip, SettingField, ToggleRow } from "./ui";

const DURATIONS = [
  { value: "off", label: "Until turned off" },
  { value: "1h", label: "For 1 hour" },
  { value: "3h", label: "For 3 hours" },
  { value: "12h", label: "For 12 hours" },
  { value: "24h", label: "For 24 hours" },
];

const LATER = "Not in v0.1: put a CDN such as Cloudflare in front of the domain";

export default function EdgeSection() {
  const [duration, setDuration] = useState("off");
  const [rules, setRules] = useState(false);
  return (
    <>
      <SettingField title="Under Attack Mode" keywords="under attack ddos bot challenge shield"
        description="Under a flood of bot traffic? Put a browser check in front of every domain of this service. People pass it once and carry on.">
        <div className="flex gap-2">
          <DropField label="Duration" className="flex-1" value={duration} options={DURATIONS} onChange={setDuration} />
          <Tooltip content={LATER}>
            <span tabIndex={0} className="inline-flex rounded-md">
              <Button size="lg" variant="danger" disabled icon={<Shield size={16} />} className="w-[120px]">Activate</Button>
            </span>
          </Tooltip>
        </div>
        <div className="mt-3 text-sm text-fg-2">Applies on every ingress host within seconds.</div>
      </SettingField>

      <SettingField title="CDN Caching" keywords="cdn cache caching static assets"
        description="Cache static files, and optionally HTML, at the ingress so repeat requests skip your service. Changes are staged and apply on deploy.">
        <ToggleRow checked={false} onChange={() => undefined} label="Enable CDN Caching" disabled reason={LATER} />
      </SettingField>

      <SettingField title="Edge Rules" keywords="edge rules redirect block challenge cache override"
        description="Block, redirect, challenge or change caching for matching requests before they reach the service. Rules run top to bottom.">
        <div className="rounded-md border border-dashed border-line p-4">
          <div className="text-base text-fg-2">No edge rules yet.</div>
          <Button className="mt-3" onClick={() => setRules(true)}>Add a rule</Button>
        </div>
      </SettingField>

      {rules && <RulesModal onClose={() => setRules(false)} />}
    </>
  );
}

function Connector() {
  return <span aria-hidden className="mx-auto block h-6 w-px bg-line" />;
}

function RulesModal({ onClose }: { onClose: () => void }) {
  return (
    <Modal title="Edge Rules" width={768} onClose={onClose} footerAlign="between" footer={<>
      <div className="flex items-center gap-3 text-caption text-fg-3">
        <span>0 of 5 rules</span>
        <NotYetChip />
      </div>
      <div className="flex gap-2">
        <Button onClick={onClose}>Cancel</Button>
        <Tooltip content={LATER}><span tabIndex={0} className="inline-flex"><Button variant="primary" disabled>Save</Button></span></Tooltip>
      </div>
    </>}>
      <p className="text-base text-fg-2">Every request to this service's domains passes through the rules in order. The first rule that matches decides what happens.</p>
      <div className="my-6 flex flex-col items-center">
        <span className="inline-flex h-7 items-center rounded-full border border-line px-3 text-caption text-fg-2">Incoming request</span>
        <Connector />
        <div className="flex w-[220px] flex-col items-center rounded-md border border-dashed border-line px-4 py-5 text-center">
          <span className="text-sm text-fg-2">No edge rules yet.</span>
          <Tooltip content={LATER}><span tabIndex={0} className="mt-3 inline-flex"><Button size="sm" disabled>Add a rule</Button></span></Tooltip>
        </div>
        <Connector />
        <ArrowDown size={14} className="-mt-1 mb-1 text-fg-4" />
        <span className="inline-flex h-7 items-center rounded-full border border-line px-3 text-caption text-fg-2">No match: the request goes to your service</span>
      </div>
    </Modal>
  );
}
