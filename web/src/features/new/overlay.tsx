// SPDX-License-Identifier: AGPL-3.0-only

// The create picker as a floating overlay, for the project canvas: the same
// GitHub, database and image steps as /new, creating into the current
// environment instead of a new project.
import { useState } from "react";
import { createPortal } from "react-dom";
import { useNavigate } from "react-router-dom";
import { canEdit, useMe } from "../../app/session";
import { ComposeFlow, ImageForm, ScanDialog, type Target } from "./flows";
import { PaletteFrame } from "./Palette";
import { DatabaseStep, GitHubStep, ImageStep, type PickerModal, type StepId, type StepProps } from "./steps";

export function PickerOverlay({ step, target, onClose, intoQuery }: {
  step: Exclude<StepId, "root" | "template">; target: Target; onClose: () => void;
  /** "?project=..&environment=.." so template links deploy into the canvas environment. */
  intoQuery?: string;
}) {
  const me = useMe();
  const nav = useNavigate();
  const [modal, setModal] = useState<PickerModal>();
  const props: StepProps = {
    dir: undefined, target, open: setModal, canCreate: canEdit(me), back: onClose,
    go: (s, text) => {
      if (s === "template") nav(`/new/template${intoQuery || "?"}${intoQuery ? "&" : ""}${text ? `q=${encodeURIComponent(text)}` : ""}`);
    },
  };
  const Step = { github: GitHubStep, database: DatabaseStep, image: ImageStep }[step];
  if (modal?.kind === "scan") return <ScanDialog target={target} repo={modal.repo} defaultBranch={modal.defaultBranch} onBack={() => setModal(undefined)} onClose={onClose} />;
  if (modal?.kind === "image") return <ImageForm target={target} initial={modal.image} onClose={onClose} />;
  if (modal?.kind === "compose") return <ComposeFlow target={target} onClose={onClose} />;
  return createPortal(
    <div className="fixed inset-0 z-[100] flex justify-center bg-black/40 px-4 pt-[18vh] animate-overlay-in"
      onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div className="h-fit animate-modal-in">
        <PaletteFrame><Step {...props} /></PaletteFrame>
      </div>
    </div>,
    document.body,
  );
}
