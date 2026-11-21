/** @type {import('tailwindcss').Config} */
export default {
  content: ["./src/studio/**/*.{js,jsx,ts,tsx,vue}"],
  // The editor ships Tailwind utilities, but ComfyStudio has its own global
  // stylesheet. Preflight is disabled and replaced by the scoped reset in
  // src/studio/styles/studio.css so the rest of the app keeps its look.
  corePlugins: {
    preflight: false,
  },
  theme: {
    extend: {
      colors: {
        border: "hsl(var(--border))",
        input: "hsl(var(--input))",
        ring: "hsl(var(--ring))",
        background: "hsl(var(--background))",
        foreground: "hsl(var(--foreground))",
        primary: {
          DEFAULT: "#2d81ff",
          foreground: "#ffffff",
        },
        secondary: {
          DEFAULT: "#3E3E41",
          foreground: "#f5f5f7",
        },
        destructive: {
          DEFAULT: "hsl(var(--destructive))",
          foreground: "hsl(var(--destructive-foreground))",
        },
        muted: {
          DEFAULT: "hsl(var(--muted))",
          foreground: "hsl(var(--muted-foreground))",
        },
        accent: {
          DEFAULT: "hsl(var(--accent))",
          foreground: "hsl(var(--accent-foreground))",
        },
        popover: {
          DEFAULT: "hsl(var(--popover))",
          foreground: "hsl(var(--popover-foreground))",
        },
        card: {
          DEFAULT: "hsl(var(--card))",
          foreground: "hsl(var(--card-foreground))",
        },
        ui: {
          background: "var(--st-bg)",
          panel: "var(--st-panel)",
          "panel-border": "var(--st-panel-border)",
          border: "var(--st-panel-border)",
          controls: "rgb(var(--st-controls-rgb) / <alpha-value>)",
          "controls-button": "rgb(var(--st-controls-rgb) / <alpha-value>)",
          "controls-border": "var(--st-controls-border)",
          divider: "var(--st-divider)",
          ink: "rgb(var(--st-fg-rgb) / <alpha-value>)",
          "accent-ink": "#74aaff",
          sunken: "var(--st-sunken)",
        },
        "base-fg": "rgb(var(--st-fg-rgb) / <alpha-value>)",
      },
      borderRadius: {
        lg: "var(--radius)",
        md: "calc(var(--radius) - 2px)",
        sm: "calc(var(--radius) - 4px)",
      },
      keyframes: {
        fadeIn: {
          "0%": { opacity: "0" },
          "100%": { opacity: "1" },
        },
      },
      animation: {
        fadeIn: "fadeIn 0.2s ease-in-out",
      },
    },
  },
  plugins: [],
  safelist: ["lucide"],
};