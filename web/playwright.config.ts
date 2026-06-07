import { defineConfig, devices } from "@playwright/test";
import path from "node:path";

const serverPort = Number(process.env.BOXFLEET_E2E_SERVER_PORT ?? "18081");
const webPort = Number(process.env.BOXFLEET_E2E_WEB_PORT ?? "4173");
const dbPath = process.env.BOXFLEET_E2E_DB ?? "/tmp/boxfleet-web-e2e.db";
const chromePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH ?? "/usr/bin/google-chrome-stable";

export default defineConfig({
  testDir: "./e2e",
  globalSetup: "./e2e/global-setup.ts",
  timeout: 45_000,
  expect: { timeout: 8_000 },
  fullyParallel: false,
  retries: process.env.CI ? 1 : 0,
  reporter: [["list"]],
  use: {
    baseURL: `http://127.0.0.1:${webPort}/admin/`,
    trace: "on-first-retry"
  },
  projects: [
    {
      name: "chromium",
      use: {
        ...devices["Desktop Chrome"],
        launchOptions: {
          executablePath: chromePath
        }
      }
    }
  ],
  webServer: [
    {
      command: [
        "go",
        "run",
        "./cmd/boxfleet-server",
        "--addr",
        `127.0.0.1:${serverPort}`,
        "--db",
        dbPath,
        "--allow-insecure-admin"
      ].join(" "),
      url: `http://127.0.0.1:${serverPort}/healthz`,
      cwd: path.resolve(".."),
      reuseExistingServer: false,
      timeout: 90_000
    },
    {
      command: `npm run dev -- --port ${webPort} --strictPort`,
      url: `http://127.0.0.1:${webPort}/admin/`,
      cwd: path.resolve("."),
      reuseExistingServer: false,
      timeout: 45_000
    }
  ]
});
