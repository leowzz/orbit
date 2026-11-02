import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./e2e",
  workers: 1,
  use: {
    baseURL: "http://127.0.0.1:17622",
    channel: process.env.ORBIT_BROWSER_CHANNEL,
    viewport: { width: 1440, height: 1000 },
    trace: "retain-on-failure",
  },
  webServer: {
    command: "go run ./e2e/fixture",
    url: "http://127.0.0.1:17622/box",
    reuseExistingServer: false,
    timeout: 60000,
  },
});
