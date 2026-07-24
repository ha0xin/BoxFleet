import js from "@eslint/js";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["dist/", "playwright-report/", "test-results/"] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      globals: { ...globals.browser, ...globals.node }
    },
    plugins: { "react-hooks": reactHooks },
    rules: {
      ...reactHooks.configs.flat.recommended.rules,
      "react-hooks/incompatible-library": "off",
      "@typescript-eslint/no-explicit-any": "error"
    }
  },
  {
    files: ["src/publish/publish-status.tsx"],
    rules: {
      // This component implements a documented transition state machine. Its
      // refs intentionally bridge query snapshots into mutation callbacks.
      "react-hooks/refs": "off",
      "react-hooks/set-state-in-effect": "off"
    }
  },
  {
    files: ["src/pages/mihomo-profiles.tsx"],
    rules: {
      // The selected pipeline is reconciled when the server replaces the list.
      "react-hooks/set-state-in-effect": "off"
    }
  },
  {
    files: ["**/*.js", "**/*.mjs"],
    languageOptions: { globals: globals.node }
  }
);
