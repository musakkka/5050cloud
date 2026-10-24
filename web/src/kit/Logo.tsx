// SPDX-License-Identifier: AGPL-3.0-only

// The digits "50" in Inter Bold, drawn on the 32 grid for the upper half; the lower half reuses
// them shifted down 16.
const FIFTY = "M12.49 11.9Q11.71 11.9 11.1 11.61Q10.49 11.33 10.13 10.82Q9.78 10.32 9.76 9.67H11.28Q11.29 9.96 11.46 10.19Q11.62 10.42 11.9 10.54Q12.17 10.67 12.49 10.67Q12.87 10.67 13.16 10.5Q13.46 10.33 13.62 10.02Q13.79 9.71 13.79 9.31Q13.79 8.91 13.62 8.59Q13.45 8.28 13.15 8.11Q12.85 7.93 12.46 7.93Q12.12 7.93 11.81 8.07Q11.49 8.2 11.32 8.43L9.92 8.19L10.28 4.2H14.88V5.49H11.57L11.38 7.41H11.42Q11.62 7.13 12.03 6.95Q12.43 6.78 12.92 6.78Q13.43 6.78 13.87 6.96Q14.3 7.14 14.62 7.48Q14.95 7.81 15.12 8.27Q15.3 8.73 15.3 9.28Q15.3 10.04 14.95 10.63Q14.59 11.22 13.96 11.56Q13.32 11.9 12.49 11.9ZM19.2 11.9Q18.24 11.9 17.56 11.44Q16.88 10.98 16.52 10.11Q16.16 9.24 16.16 8.01Q16.16 6.78 16.52 5.9Q16.88 5.03 17.56 4.56Q18.24 4.1 19.2 4.1Q20.16 4.1 20.84 4.56Q21.52 5.03 21.88 5.9Q22.24 6.78 22.24 8.01Q22.24 9.24 21.88 10.11Q21.52 10.98 20.84 11.44Q20.16 11.9 19.2 11.9ZM19.2 10.61Q19.67 10.61 19.99 10.31Q20.31 10 20.48 9.42Q20.66 8.84 20.66 8.01Q20.66 7.17 20.48 6.58Q20.31 6 19.99 5.69Q19.67 5.38 19.2 5.38Q18.73 5.38 18.41 5.69Q18.08 6 17.91 6.58Q17.74 7.17 17.74 8.01Q17.74 8.84 17.91 9.42Q18.08 10 18.41 10.31Q18.73 10.61 19.2 10.61Z";

// 5050cloud's mark: one tile split into two equal halves, each holding "50". The accent half
// carries white digits like a primary button; the ink half carries accent-text digits. A
// hairline in currentColor keeps the ink half visible on a dark page and vanishes on a light one.
// The colours are fixed so the mark reads the same in both themes.
export function Logo({ size = 24 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" role="img" aria-label="5050cloud">
      <path d="M0 16h32v10a6 6 0 0 1-6 6H6a6 6 0 0 1-6-6z" fill="#13111c" />
      <rect x="0.5" y="0.5" width="31" height="31" rx="5.5" fill="none" stroke="currentColor" strokeOpacity="0.16" />
      <path d="M0 6a6 6 0 0 1 6-6h20a6 6 0 0 1 6 6v10H0z" fill="#d5348a" />
      <path d={FIFTY} fill="#ffffff" />
      <path d={FIFTY} fill="#ea66ad" transform="translate(0 16)" />
    </svg>
  );
}
