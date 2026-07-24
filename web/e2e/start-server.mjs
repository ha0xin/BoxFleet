import { spawn } from "node:child_process";
import path from "node:path";

const [serverPort, dbPath] = process.argv.slice(2);
if (!serverPort || !dbPath) {
  throw new Error("usage: node e2e/start-server.mjs <port> <database-path>");
}

const repositoryRoot = path.resolve(import.meta.dirname, "../..");
const server = spawn(
  "go",
  [
    "run",
    "./cmd/bfs",
    "--addr",
    `127.0.0.1:${serverPort}`,
    "--db",
    dbPath,
    "--allow-insecure-admin"
  ],
  { cwd: repositoryRoot, stdio: "inherit" }
);

for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => server.kill(signal));
}
server.on("exit", (code) => process.exit(code ?? 0));
