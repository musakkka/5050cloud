// SPDX-License-Identifier: AGPL-3.0-only

// 5050cloud's mark: a cloud split down the middle, half and half.
export default function Logo({ size = 24 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" aria-label="5050cloud">
      <path d="M15 25H9.5a6.5 6.5 0 0 1-.9-12.94A8 8 0 0 1 15 7.06z" fill="currentColor" />
      <path d="M17 7.06a8 8 0 0 1 6.4 5A6.5 6.5 0 0 1 22.5 25H17z" fill="var(--pink-600)" />
    </svg>
  );
}

export function Wordmark() {
  return <span className="text-[15px] font-semibold tracking-tight">5050<span className="text-gray-500">cloud</span></span>;
}
