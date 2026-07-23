import { defineConfig, devices } from "@playwright/test";
import os from "node:os";
import path from "node:path";

const serverPort = Number(process.env.BOXFLEET_E2E_SERVER_PORT ?? "18081");
const webPort = Number(process.env.BOXFLEET_E2E_WEB_PORT ?? "4173");
const dbPath = process.env.BOXFLEET_E2E_DB ?? path.join(os.tmpdir(), `boxfleet-web-e2e-${process.pid}.db`);
const defaultChromePath = process.platform === "darwin"
  ? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
  : "/usr/bin/google-chrome-stable";
const chromePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH ?? defaultChromePath;

export default defineConfig({
  testDir: "./e2e",
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
        `go run ./cmd/bf --db ${JSON.stringify(dbPath)} db init`,
        "&&",
        "go run ./cmd/boxfleet-server",
        "--addr",
        `127.0.0.1:${serverPort}`,
        "--db",
        JSON.stringify(dbPath),
        "--allow-insecure-admin"
      ].join(" "),
      url: `http://127.0.0.1:${serverPort}/healthz`,
      cwd: path.resolve(".."),
      reuseExistingServer: false,
      timeout: 90_000
    },
    {
      command: `npm run dev:api -- --port ${webPort} --strictPort`,
      url: `http://127.0.0.1:${webPort}/admin/`,
      cwd: path.resolve("."),
      reuseExistingServer: false,
      timeout: 45_000
    }
  ]
});
