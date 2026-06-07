import path from "node:path";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  base: "/admin/",
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src")
    }
  },
  build: {
    outDir: "../internal/server/webui/assets/generated",
    emptyOutDir: true
  },
  server: {
    proxy: {
      "/api": "http://127.0.0.1:18081"
    }
  }
});
