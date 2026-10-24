// SPDX-License-Identifier: AGPL-3.0-only

// The canvas behind the create picker: a fine halftone dot field with two
// dotted mountain ridges along the bottom, fading up into the page. Pure SVG
// and CSS so it themes with the tokens and costs nothing to load.
const FAR = "M0 300 L120 236 L190 262 L300 150 L360 188 L430 120 L520 214 L610 176 L700 236 L820 160 L905 210 L990 128 L1080 196 L1170 154 L1260 222 L1360 140 L1450 200 L1530 170 L1600 210 L1600 520 L0 520 Z";
const NEAR = "M0 392 L90 350 L170 376 L260 318 L350 360 L450 302 L560 368 L660 334 L760 388 L870 330 L980 380 L1090 318 L1190 364 L1290 332 L1400 378 L1500 342 L1600 370 L1600 520 L0 520 Z";

export function Backdrop() {
  return (
    <div aria-hidden className="pointer-events-none absolute inset-0 overflow-hidden bg-background">
      <div className="absolute inset-0 opacity-60"
        style={{ backgroundImage: "radial-gradient(circle at 1px 1px, var(--dot-grid) .75px, transparent 0)", backgroundSize: "5px 5px" }} />
      <div className="absolute inset-x-0 bottom-0 h-[56%]"
        style={{ maskImage: "linear-gradient(to bottom, transparent 0%, #000 55%)", WebkitMaskImage: "linear-gradient(to bottom, transparent 0%, #000 55%)" }}>
        <svg className="absolute inset-0 h-full w-full" viewBox="0 0 1600 520" preserveAspectRatio="xMidYMax slice">
          <defs>
            <pattern id="bd-far" width="5" height="5" patternUnits="userSpaceOnUse"><circle cx="2.5" cy="2.5" r="1.3" fill="var(--fg-5)" /></pattern>
            <pattern id="bd-near" width="5" height="5" patternUnits="userSpaceOnUse"><circle cx="2.5" cy="2.5" r="1.5" fill="var(--line)" /></pattern>
            <pattern id="bd-snow" width="5" height="5" patternUnits="userSpaceOnUse"><circle cx="2.5" cy="2.5" r="1.1" fill="var(--pink-300)" /></pattern>
          </defs>
          <path d={FAR} fill="var(--background)" />
          <path d={FAR} fill="url(#bd-far)" opacity=".45" />
          <path opacity=".35" fill="url(#bd-snow)"
            d="M300 150 L360 188 L430 120 L470 162 L430 152 L390 176 L360 196 Z M990 128 L1040 166 L1010 160 L985 176 L960 152 Z M1360 140 L1410 172 L1380 168 L1350 184 L1330 160 Z" />
          <path d={NEAR} fill="var(--background)" />
          <path d={NEAR} fill="url(#bd-near)" opacity=".75" />
        </svg>
      </div>
    </div>
  );
}
