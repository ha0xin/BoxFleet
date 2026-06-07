import fs from "node:fs/promises";

export default async function globalSetup() {
  const dbPath = process.env.BOXFLEET_E2E_DB ?? "/tmp/boxfleet-web-e2e.db";
  await Promise.all([
    fs.rm(dbPath, { force: true }),
    fs.rm(`${dbPath}-shm`, { force: true }),
    fs.rm(`${dbPath}-wal`, { force: true })
  ]);
}
