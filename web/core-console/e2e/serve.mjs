// Exercise the shipped Nginx gateway against a disposable Core API fixture.
import { spawn, spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../", import.meta.url));
const endpoint =
  process.env.DOCKER_HOST ||
  spawnSync(
    "docker",
    ["context", "inspect", "--format", "{{.Endpoints.docker.Host}}"],
    { encoding: "utf8" },
  ).stdout?.trim();
if (!endpoint || !/^(unix|npipe):/.test(endpoint)) {
  throw new Error(
    "Browser tests require a local Docker daemon. Select one with DOCKER_CONTEXT (for example, colima).",
  );
}
const name = `orbit-console-check-${process.pid}`;
const backend = spawn("go", ["run", "./e2e/fixture"], {
  cwd: root,
  stdio: "inherit",
  detached: true,
});
let gateway;
let closing = false;
function close(code = 0) {
  if (closing) return;
  closing = true;
  spawnSync("docker", ["rm", "-f", name], { stdio: "ignore" });
  try {
    process.kill(-backend.pid, "SIGTERM");
  } catch {
    /* already exited */
  }
  process.exitCode = code;
}
process.on("SIGTERM", () => close());
process.on("SIGINT", () => close());
backend.on("error", (error) => {
  console.error(error);
  close(1);
});
backend.on("exit", (code) => close(code || 1));

for (let attempt = 0; attempt < 200 && !closing; attempt++) {
  try {
    const response = await fetch("http://127.0.0.1:17623/api/auth/session");
    if (response.status === 401) break;
  } catch {
    /* fixture is starting */
  }
  if (attempt === 199) {
    console.error("API fixture did not start");
    close(1);
  }
  await new Promise((resolve) => setTimeout(resolve, 100));
}
if (!closing) {
  gateway = spawn(
    "docker",
    [
      "run",
      "--rm",
      "--name",
      name,
      "--cpus",
      "2",
      "-e",
      "NGINX_ENTRYPOINT_WORKER_PROCESSES_AUTOTUNE=1",
      "--add-host",
      "host.docker.internal:host-gateway",
      "-p",
      "127.0.0.1:17622:8080",
      "-v",
      `${root}dist:/usr/share/nginx/html:ro`,
      "-v",
      `${root}nginx.conf.template:/etc/nginx/templates/default.conf.template:ro`,
      "-e",
      "ORBIT_CORE_UPSTREAM=host.docker.internal:17623",
      "nginx:1.28-alpine",
    ],
    { stdio: "inherit" },
  );
  gateway.on("error", (error) => {
    console.error(error);
    close(1);
  });
  gateway.on("exit", (code) => close(code || 1));
}
