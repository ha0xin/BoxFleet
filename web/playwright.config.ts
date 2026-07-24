import { defineConfig, devices } from "@playwright/test";
import { existsSync } from "node:fs";
import os from "node:os";
import path from "node:path";

const serverPort = Number(process.env.BOXFLEET_E2E_SERVER_PORT ?? "18081");
const webPort = Number(process.env.BOXFLEET_E2E_WEB_PORT ?? "4173");
const dbPath = process.env.BOXFLEET_E2E_DB ?? path.join(os.tmpdir(), `boxfleet-web-e2e-${process.pid}.db`);
const browserNames = new Set(
  (process.env.BOXFLEET_E2E_BROWSERS ?? "chromium")
    .split(",")
    .map((name) => name.trim())
    .filter(Boolean)
);

function installedChromePath(): string | undefined {
  const configured = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH;
  if (configured) {
    if (!existsSync(configured)) throw new Error(`Chrome executable does not exist: ${configured}`);
    return configured;
  }
  const candidates = process.platform === "darwin"
    ? ["/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"]
    : process.platform === "win32"
      ? [
          process.env.PROGRAMFILES && path.join(process.env.PROGRAMFILES, "Google/Chrome/Application/chrome.exe"),
          process.env["PROGRAMFILES(X86)"] && path.join(process.env["PROGRAMFILES(X86)"], "Google/Chrome/Application/chrome.exe"),
          process.env.LOCALAPPDATA && path.join(process.env.LOCALAPPDATA, "Google/Chrome/Application/chrome.exe")
        ]
      : ["/usr/bin/google-chrome-stable", "/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser"];
  return candidates.find((candidate): candidate is string => Boolean(candidate && existsSync(candidate)));
}

const chromePath = installedChromePath();
const projects = [
  {
    name: "chromium",
    use: {
      ...devices["Desktop Chrome"],
      launchOptions: chromePath ? { executablePath: chromePath } : undefined
    }
  },
  { name: "firefox", use: { ...devices["Desktop Firefox"] } },
  { name: "webkit", use: { ...devices["Desktop Safari"] } }
].filter((project) => browserNames.has(project.name));

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
  projects,
  webServer: [
    {
      command: `node ./e2e/start-server.mjs ${serverPort} ${JSON.stringify(dbPath)}`,
      url: `http://127.0.0.1:${serverPort}/healthz`,
      cwd: path.resolve("."),
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
