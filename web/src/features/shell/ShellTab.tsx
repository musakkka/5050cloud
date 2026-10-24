// SPDX-License-Identifier: AGPL-3.0-only

// The service panel's Console tab (docs/SPEC.md 13.3): a live terminal in one
// running instance, with the Files drawer. The lazy import in ServicePanel
// loads this module, so it is the only entry point the panel needs.

import type { Service } from "../../api/types.gen";
import { ConsoleSurface } from "./ConsoleSurface";

export default function ShellTab({ eid, service }: { eid: string; service: Service }) {
  return (
    <ConsoleSurface
      variant="panel"
      target={{ eid, sid: service.id, name: service.config.name, replicas: service.replicas }}
    />
  );
}
