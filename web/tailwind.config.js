// SPDX-License-Identifier: AGPL-3.0-only

// 5050cloud console palette: neutral greys and a pink accent, defined as CSS custom properties in src/index.css.
const scale = (name) =>
  Object.fromEntries([50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950].map((k) => [k, `var(--${name}-${k})`]));

/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        foreground: "var(--foreground)",
        background: "var(--background)",
        secondaryBg: "var(--secondaryBg)",
        gray: scale("gray"),
        blue: scale("blue"),
        red: scale("red"),
        pink: scale("pink"),
        green: scale("green"),
        yellow: scale("yellow"),
        success: "hsl(152, 38%, 60%)",
      },
      fontFamily: {
        sans: ["Inter", "-apple-system", "BlinkMacSystemFont", "Segoe UI", "sans-serif"],
        mono: ["ui-monospace", "SFMono-Regular", "Menlo", "Monaco", "Consolas", "monospace"],
      },
      height: { md: "34px" },
    },
  },
  plugins: [],
};
