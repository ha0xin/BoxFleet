const { fontFamily } = require("tailwindcss/defaultTheme");

/** @type {import('tailwindcss').Config} */
module.exports = {
  darkMode: ["class"],
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        border: "hsl(var(--border))",
        ring: "hsl(var(--ring))",
        foreground: "hsl(var(--foreground))",
        background: {
          100: "hsl(var(--ds-background-100))",
          200: "hsl(var(--ds-background-200))"
        },
        contrast: "hsl(var(--ds-contrast-fg))",
        accents: {
          1: "var(--accents-1)",
          2: "var(--accents-2)",
          3: "var(--accents-3)",
          4: "var(--accents-4)",
          5: "var(--accents-5)",
          6: "var(--accents-6)",
          7: "var(--accents-7)",
          8: "var(--accents-8)"
        },
        gray: {
          100: "hsl(var(--ds-gray-100))",
          200: "hsl(var(--ds-gray-200))",
          300: "hsl(var(--ds-gray-300))",
          400: "hsl(var(--ds-gray-400))",
          500: "hsl(var(--ds-gray-500))",
          600: "hsl(var(--ds-gray-600))",
          700: "hsl(var(--ds-gray-700))",
          800: "hsl(var(--ds-gray-800))",
          900: "hsl(var(--ds-gray-900))",
          1000: "hsl(var(--ds-gray-1000))"
        },
        "gray-alpha": {
          100: "var(--ds-gray-alpha-100)",
          200: "var(--ds-gray-alpha-200)",
          300: "var(--ds-gray-alpha-300)",
          400: "var(--ds-gray-alpha-400)",
          500: "var(--ds-gray-alpha-500)",
          600: "var(--ds-gray-alpha-600)",
          700: "var(--ds-gray-alpha-700)",
          800: "var(--ds-gray-alpha-800)",
          900: "var(--ds-gray-alpha-900)",
          1000: "var(--ds-gray-alpha-1000)"
        },
        blue: {
          100: "hsl(var(--ds-blue-100))",
          200: "hsl(var(--ds-blue-200))",
          300: "hsl(var(--ds-blue-300))",
          400: "hsl(var(--ds-blue-400))",
          500: "hsl(var(--ds-blue-500))",
          600: "hsl(var(--ds-blue-600))",
          700: "hsl(var(--ds-blue-700))",
          800: "hsl(var(--ds-blue-800))",
          900: "hsl(var(--ds-blue-900))",
          1000: "hsl(var(--ds-blue-1000))"
        },
        red: {
          100: "hsl(var(--ds-red-100))",
          200: "hsl(var(--ds-red-200))",
          300: "hsl(var(--ds-red-300))",
          400: "hsl(var(--ds-red-400))",
          500: "hsl(var(--ds-red-500))",
          600: "hsl(var(--ds-red-600))",
          700: "hsl(var(--ds-red-700))",
          800: "hsl(var(--ds-red-800))",
          900: "hsl(var(--ds-red-900))",
          1000: "hsl(var(--ds-red-1000))"
        },
        amber: {
          100: "hsl(var(--ds-amber-100))",
          200: "hsl(var(--ds-amber-200))",
          300: "hsl(var(--ds-amber-300))",
          400: "hsl(var(--ds-amber-400))",
          500: "hsl(var(--ds-amber-500))",
          600: "hsl(var(--ds-amber-600))",
          700: "hsl(var(--ds-amber-700))",
          800: "hsl(var(--ds-amber-800))",
          900: "hsl(var(--ds-amber-900))",
          1000: "hsl(var(--ds-amber-1000))"
        },
        green: {
          100: "hsl(var(--ds-green-100))",
          200: "hsl(var(--ds-green-200))",
          300: "hsl(var(--ds-green-300))",
          400: "hsl(var(--ds-green-400))",
          500: "hsl(var(--ds-green-500))",
          600: "hsl(var(--ds-green-600))",
          700: "hsl(var(--ds-green-700))",
          800: "hsl(var(--ds-green-800))",
          900: "hsl(var(--ds-green-900))",
          1000: "hsl(var(--ds-green-1000))"
        },
        teal: {
          100: "hsl(var(--ds-teal-100))",
          200: "hsl(var(--ds-teal-200))",
          300: "hsl(var(--ds-teal-300))",
          400: "hsl(var(--ds-teal-400))",
          500: "hsl(var(--ds-teal-500))",
          600: "hsl(var(--ds-teal-600))",
          700: "hsl(var(--ds-teal-700))",
          800: "hsl(var(--ds-teal-800))",
          900: "hsl(var(--ds-teal-900))",
          1000: "hsl(var(--ds-teal-1000))"
        },
        purple: {
          100: "hsl(var(--ds-purple-100))",
          200: "hsl(var(--ds-purple-200))",
          300: "hsl(var(--ds-purple-300))",
          400: "hsl(var(--ds-purple-400))",
          500: "hsl(var(--ds-purple-500))",
          600: "hsl(var(--ds-purple-600))",
          700: "hsl(var(--ds-purple-700))",
          800: "hsl(var(--ds-purple-800))",
          900: "hsl(var(--ds-purple-900))",
          1000: "hsl(var(--ds-purple-1000))"
        },
        pink: {
          100: "hsl(var(--ds-pink-100))",
          200: "hsl(var(--ds-pink-200))",
          300: "hsl(var(--ds-pink-300))",
          400: "hsl(var(--ds-pink-400))",
          500: "hsl(var(--ds-pink-500))",
          600: "hsl(var(--ds-pink-600))",
          700: "hsl(var(--ds-pink-700))",
          800: "hsl(var(--ds-pink-800))",
          900: "hsl(var(--ds-pink-900))",
          1000: "hsl(var(--ds-pink-1000))"
        }
      },
      boxShadow: {
        inner: "var(--ds-shadow-inset)",
        border: "var(--ds-shadow-border)",
        small: "var(--ds-shadow-small)",
        ["border-small"]: "var(--ds-shadow-border-small)",
        ["input-ring"]: "var(--ds-shadow-input-ring)",
        medium: "var(--ds-shadow-medium)",
        large: "var(--ds-shadow-large)",
        ["border-large"]: "var(--ds-shadow-border-large)",
        tooltip: "var(--ds-shadow-tooltip)",
        menu: "var(--ds-shadow-menu)",
        modal: "var(--ds-shadow-modal)"
      },
      fontFamily: {
        sans: ["Inter", "system-ui", ...fontFamily.sans]
      },
      keyframes: {
        "spinner-spin": {
          "0%": { opacity: "1" },
          "100%": { opacity: "0.15" }
        },
        "copy-button-fadeIn": {
          "0%": { opacity: "0", scale: "0.5" },
          "100%": { opacity: "1", scale: "1" }
        },
        "copy-button-fadeOut": {
          "0%": { opacity: "1", scale: "1" },
          "100%": { opacity: "0", scale: "0.5" }
        },
        "loading-dots-blink": {
          "0%, 100%": { opacity: "0.2" },
          "20%": { opacity: "1" }
        }
      },
      animation: {
        "spinner-spin": "spinner-spin 1.2s linear infinite",
        "copy-button-fadeIn": "copy-button-fadeIn 150ms ease-out forwards",
        "copy-button-fadeOut": "copy-button-fadeOut 150ms ease-out forwards",
        "loading-dots-blink": "loading-dots-blink 1400ms both infinite"
      }
    }
  },
  plugins: []
};
