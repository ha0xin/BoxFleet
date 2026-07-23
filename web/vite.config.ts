import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

import { adminMockPlugin } from "./mocks/admin";

// By default `npm run dev` serves mock admin data so the UI is populated without
// a running boxfleet-server. Set BOXFLEET_DEV_API=1 (or use `npm run dev:api`) to
// proxy /api to a real server on :18081 instead.
const useRealApi = process.env.BOXFLEET_DEV_API === "1";

export default defineConfig({
  base: "/admin/",
  plugins: [tailwindcss(), react(), ...(useRealApi ? [] : [adminMockPlugin()])],
  resolve: {
    alias: [
      // monaco-yaml's worker manager still imports Monaco's documented ESM path;
      // Monaco 0.56's export map no longer exposes that spelling to Vite.
      {
        find: /^monaco-editor\/esm\/vs\/(.*)$/,
        replacement: path.resolve(__dirname, "node_modules/monaco-editor/esm/vs/$1")
      },
      { find: "@", replacement: path.resolve(__dirname, "./src") }
    ]
  },
  build: {
    outDir: "../internal/server/webui/assets/generated",
    emptyOutDir: true
  },
  server: useRealApi
    ? {
        proxy: {
          "/api": "http://127.0.0.1:18081"
        }
      }
    : undefined
});
