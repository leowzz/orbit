import { test, expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import { writeFileSync } from "node:fs";

const pixel =
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGP4z8DwHwAFAAH/iZk9HQAAAABJRU5ErkJggg==";

// Close the page-owned SSE before Playwright finalizes network traces.
// Browser disposal alone can leave a streaming response pending in tracing.
test.afterEach(async ({ page }) => {
  if (!page.isClosed())
    await page
      .evaluate(() => window.dispatchEvent(new Event("orbit:logout")))
      .catch(() => {});
});

async function login(page: Page, path = "/box") {
  await page.goto(path);
  await page.getByLabel("工作台密码").fill("local-check-only");
  await page.getByRole("button", { name: "进入工作台" }).click();
  await expect(page.locator("#sync-state")).toHaveText("已同步");
}
async function send(page: Page, body: string) {
  await page.getByLabel("消息内容", { exact: true }).fill(body);
  await page.getByRole("button", { name: "发送", exact: true }).click();
  await expect(
    page.locator(".message").filter({ hasText: body }),
  ).toBeVisible();
  await expect(page.locator("#sync-state")).toHaveText("已同步");
  await expect(
    page.locator(".message").filter({ hasText: body }),
  ).not.toContainText("待同步");
}

test("direct links, refresh, history, inbox actions and responsive layout", async ({
  page,
  context,
}, testInfo) => {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await login(page);
  await send(page, "浏览器验收 https://example.org/check");
  const message = page.locator(".message").filter({ hasText: "浏览器验收" });
  await expect(message.getByRole("link")).toHaveAttribute(
    "href",
    "https://example.org/check",
  );
  await message.getByRole("button", { name: "复制", exact: true }).click();
  await expect
    .poll(() => page.evaluate(() => navigator.clipboard.readText()))
    .toContain("浏览器验收");
  await message.locator("summary").click();
  await message.getByRole("button", { name: "设为待办", exact: true }).click();
  await expect(
    message.getByRole("checkbox", { name: "完成待办" }),
  ).toBeVisible();
  await message.getByRole("checkbox", { name: "完成待办" }).check();
  await expect(message).toHaveClass(/completed/);
  await page.getByLabel("筛选消息").selectOption("completed");
  await expect(message).toBeVisible();
  await message.locator("summary").click();
  await message.getByRole("button", { name: "编辑", exact: true }).click();
  await page.getByLabel("编辑消息内容").fill("编辑后的浏览器验收");
  await page.getByRole("button", { name: "保存", exact: true }).click();
  const edited = page
    .locator(".message")
    .filter({ hasText: "编辑后的浏览器验收" });
  await expect(edited).toBeVisible();
  await expect(edited).not.toContainText("待同步");
  page.once("dialog", (dialog) => dialog.accept());
  await edited.locator("summary").click();
  await edited.getByRole("button", { name: "删除", exact: true }).click();
  await expect(edited).toHaveCount(0);
  await page.getByLabel("筛选消息").selectOption("deleted");
  await expect(edited).toBeVisible();
  await edited.locator("summary").click();
  await edited.getByRole("button", { name: "恢复", exact: true }).click();
  await expect(edited).toHaveCount(0);
  await page.getByLabel("筛选消息").selectOption("all");
  await expect(edited).toBeVisible();
  await page.reload();
  await expect(edited).toBeVisible();
  await page.getByRole("link", { name: "总览", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "工作台", exact: true }),
  ).toBeVisible();
  await page.goBack();
  await expect(
    page.getByRole("heading", { name: "收件箱", exact: true }),
  ).toBeVisible();
  await expect(page.locator("#send")).toBeEnabled();
  await page.getByRole("button", { name: "搜索消息", exact: true }).click();
  await page.getByRole("searchbox").fill("不会匹配的文本");
  await expect(page.getByText("没有找到匹配的消息")).toBeVisible();
  await page.getByRole("searchbox").press("Escape");
  await page.screenshot({
    path: testInfo.outputPath("desktop.png"),
    fullPage: true,
    animations: "disabled",
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.getByRole("button", { name: "打开导航" })).toBeVisible();
  await expect(page.locator("#main-navigation")).toHaveAttribute("inert", "");
  await expect
    .poll(async () => {
      const navigation = await page.locator("#main-navigation").boundingBox();
      return Math.round(navigation!.x + navigation!.width);
    })
    .toBeLessThanOrEqual(0);
  await expect(edited).toBeVisible();
  await send(page, "窄屏发送验收");
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth))
    .toBe(390);
  const textarea = await page.locator("#body").boundingBox();
  const sendButton = await page.locator("#send").boundingBox();
  expect(textarea!.x).toBeGreaterThanOrEqual(0);
  expect(sendButton!.x + sendButton!.width).toBeLessThanOrEqual(390);
  await page.screenshot({
    path: testInfo.outputPath("mobile.png"),
    fullPage: true,
    animations: "disabled",
  });
  await page.getByRole("button", { name: "打开导航" }).click();
  await expect(page.locator("#main-navigation")).not.toHaveAttribute(
    "inert",
    "",
  );
  await page.getByRole("link", { name: "总览", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "工作台", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "切换到深色模式" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect
    .poll(async () => {
      const navigation = await page.locator("#main-navigation").boundingBox();
      return Math.round(navigation!.x + navigation!.width);
    })
    .toBeLessThanOrEqual(0);
  await page.screenshot({
    path: testInfo.outputPath("mobile-home-dark.png"),
    fullPage: true,
    animations: "disabled",
  });
  for (const path of ["/messages", "/apps", "/inbox/", "/box/"]) {
    await page.goto(path);
    await expect(
      page.getByRole("heading", { name: "收件箱", exact: true }),
    ).toBeVisible();
  }
  expect(errors).toEqual([]);
});

test("draft durability, offline outbox, cross-tab lock, and logout", async ({
  page,
  context,
}) => {
  await login(page);
  await page.getByLabel("消息内容", { exact: true }).fill("保留的草稿");
  await page.reload();
  await expect(page.getByLabel("消息内容", { exact: true })).toHaveValue(
    "保留的草稿",
  );
  const other = await context.newPage();
  await other.goto("/box");
  await expect(other.locator("#error")).toContainText("另一个标签页");
  await expect(other.locator("#send")).toBeDisabled();
  await other.close();
  await context.setOffline(true);
  await page.getByRole("button", { name: "发送", exact: true }).click();
  await expect(
    page.locator(".message").filter({ hasText: "保留的草稿" }),
  ).toContainText("待同步");
  await context.setOffline(false);
  await page.getByRole("button", { name: "同步消息" }).click();
  await expect(page.locator("#sync-state")).toHaveText("已同步");
  await expect(
    page.locator(".message").filter({ hasText: "保留的草稿" }),
  ).not.toContainText("待同步");
  await page.getByLabel("消息内容", { exact: true }).fill("退出前的草稿");
  await page.getByRole("link", { name: "总览", exact: true }).click();
  page.once("dialog", (dialog) => dialog.dismiss());
  await page.getByRole("button", { name: "退出登录", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "工作台", exact: true }),
  ).toBeVisible();
  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "退出登录", exact: true }).click();
  await expect(page.getByRole("heading", { name: "登录工作台" })).toBeVisible();
  await login(page);
  await expect(page.getByLabel("消息内容", { exact: true })).toHaveValue("");
});

test("conflicts retain edits and server failures do not appear empty", async ({
  page,
}) => {
  await login(page);
  await send(page, "需要冲突处理");
  const message = page.locator(".message").filter({ hasText: "需要冲突处理" });
  await message.locator("summary").click();
  await message.getByRole("button", { name: "编辑", exact: true }).click();
  await page.getByLabel("编辑消息内容").fill("本地冲突修改");
  await page.route("**/api/app/operations", (route) =>
    route.fulfill({
      status: 409,
      contentType: "application/json",
      body: '{"code":"conflict"}',
    }),
  );
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(page.locator(".draft-error")).toContainText("本地冲突修改");
  await expect(message).toBeVisible();
  await page.unroute("**/api/app/operations");
  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "放弃修改" }).click();
  await page.route("**/api/app/changes?**", (route) =>
    route.fulfill({ status: 503, body: '{"code":"unavailable"}' }),
  );
  await page.getByRole("button", { name: "同步消息" }).click();
  await expect(page.locator("#error")).toBeVisible();
  await expect(message).toBeVisible();
  await expect(page.getByText("给之后的自己留点东西")).toHaveCount(0);
});

test("image selection, preview and narrow editor", async ({ page }) => {
  await login(page);
  await page.locator("#photo").setInputFiles({
    name: "unsupported.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("not an image"),
  });
  await expect(page.locator("#toast")).toContainText(
    "请选择 JPEG、PNG 或 GIF 图片",
  );
  await page.locator("#photo").setInputFiles({
    name: "pixel.png",
    mimeType: "image/png",
    buffer: Buffer.from(pixel, "base64"),
  });
  await page.getByLabel("消息内容", { exact: true }).fill("图片验收");
  await page.getByLabel("消息内容", { exact: true }).press("Control+Enter");
  const image = page.locator(".message").filter({ hasText: "图片验收" });
  await expect(image).toBeVisible();
  await expect(image).not.toContainText("待同步");
  await expect(image.locator("img")).toBeVisible();
  await image.locator(".photo-button").click();
  await expect(page.locator("#image-view")).toBeVisible();
  await expect
    .poll(() =>
      page
        .locator("#full-image")
        .evaluate((image) => (image as HTMLImageElement).naturalWidth),
    )
    .toBeGreaterThan(0);
  await page.getByRole("button", { name: "关闭图片" }).click();
  await page.setViewportSize({ width: 390, height: 844 });
  await image.locator("summary").click();
  await image.getByRole("button", { name: "编辑", exact: true }).click();
  await expect(page.locator("#editor")).toBeVisible();
  await expect
    .poll(() => page.evaluate(() => document.documentElement.scrollWidth))
    .toBe(390);
});

test("pasted and dropped images survive offline synchronization", async ({
  page,
  context,
}) => {
  await login(page);
  await context.setOffline(true);
  for (const action of ["paste", "drop"]) {
    await page.evaluate(
      ({ action, pixel }) => {
        const transfer = new DataTransfer();
        transfer.items.add(
          new File(
            [Uint8Array.from(atob(pixel), (char) => char.charCodeAt(0))],
            action + ".png",
            { type: "image/png" },
          ),
        );
        if (action === "paste")
          document.querySelector("#body")!.dispatchEvent(
            new ClipboardEvent("paste", {
              clipboardData: transfer,
              bubbles: true,
              cancelable: true,
            }),
          );
        else
          document.querySelector("#composer")!.dispatchEvent(
            new DragEvent("drop", {
              dataTransfer: transfer,
              bubbles: true,
              cancelable: true,
            }),
          );
      },
      { action, pixel },
    );
    await expect(page.locator("#attachment-name")).toHaveText(action + ".png");
    await page
      .getByLabel("消息内容", { exact: true })
      .fill(action + " 离线图片");
    await page.getByRole("button", { name: "发送", exact: true }).click();
    const message = page
      .locator(".message")
      .filter({ hasText: action + " 离线图片" });
    await expect(message).toContainText("待同步");
    await expect
      .poll(() =>
        message
          .locator("img")
          .evaluate((image) => (image as HTMLImageElement).naturalWidth),
      )
      .toBeGreaterThan(0);
  }
  await expect(page.locator("#error")).toBeVisible();
  await context.setOffline(false);
  await page.getByRole("button", { name: "同步消息" }).click();
  await expect(page.locator("#sync-state")).toHaveText("已同步");
  await expect(
    page.locator(".message").filter({ hasText: "离线图片" }),
  ).toHaveCount(2);
  await expect(page.locator("#messages")).not.toContainText("待同步");
});

test("expired sessions return to login and retain the local draft", async ({
  page,
}) => {
  await login(page);
  await page.getByLabel("消息内容", { exact: true }).fill("重新登录后继续");
  await page.request.post("/api/auth/logout");
  await page.getByRole("button", { name: "同步消息" }).click();
  await expect(page.getByRole("heading", { name: "登录工作台" })).toBeVisible();
  await page.getByLabel("工作台密码").fill("local-check-only");
  await page.getByRole("button", { name: "进入工作台" }).click();
  await expect(page.getByLabel("消息内容", { exact: true })).toHaveValue(
    "重新登录后继续",
  );
  await expect(page.locator("#sync-state")).toHaveText("已同步");
});

test("cold load only fetches inbox data and repeat navigation reuses assets", async ({
  page,
}, testInfo) => {
  // Authorize before navigating so the timed page is a direct authenticated load.
  await page.request.post("/api/auth/login", {
    data: { password: "local-check-only" },
  });
  const requests: string[] = [];
  page.on("request", (request) =>
    requests.push(new URL(request.url()).pathname),
  );
  const started = Date.now();
  await page.goto("/box");
  await expect(page.locator("#sync-state")).toHaveText("已同步");
  const readyMs = Date.now() - started;
  const resources = await page.evaluate(() =>
    performance.getEntriesByType("resource").map((entry) => {
      const resource = entry as PerformanceResourceTiming;
      return {
        name: new URL(resource.name).pathname,
        transferSize: resource.transferSize,
        encodedBodySize: resource.encodedBodySize,
        decodedBodySize: resource.decodedBodySize,
      };
    }),
  );
  expect(requests).not.toContain("/api/state");
  expect(requests).not.toContain("/api/routes");
  expect(requests).not.toContain("/api/app/devices");
  expect(
    requests.filter((path) => /Audio|Reviews|Nodes|DeviceAccess/.test(path)),
  ).toEqual([]);
  const scripts = resources.filter((resource) => resource.name.endsWith(".js"));
  const decodedJS = scripts.reduce(
    (sum, resource) => sum + resource.decodedBodySize,
    0,
  );
  const encodedJS = scripts.reduce(
    (sum, resource) => sum + resource.encodedBodySize,
    0,
  );
  expect(decodedJS).toBeLessThan(300000);
  expect(encodedJS).toBeLessThan(100000);
  const coldRequests = [...requests];
  await page.reload();
  await expect(page.locator("#sync-state")).toHaveText("已同步");
  const repeat = await page.evaluate(() =>
    performance
      .getEntriesByType("resource")
      .filter((entry) => entry.name.includes("/assets/"))
      .map((entry) => ({
        name: entry.name,
        transferSize: (entry as PerformanceResourceTiming).transferSize,
      })),
  );
  expect(repeat.length).toBeGreaterThan(0);
  expect(repeat.every((resource) => resource.transferSize === 0)).toBe(true);
  writeFileSync(
    testInfo.outputPath("performance.json"),
    JSON.stringify(
      {
        readyMs,
        decodedJS,
        encodedJS,
        requests: coldRequests,
        resources,
        repeat,
      },
      null,
      2,
    ),
  );
});

test("compact overview renders response data, grouped navigation and readable rows", async ({
  page,
  context,
  browser,
}, testInfo) => {
  const timestamp = new Date().toISOString();
  const computer = "验收电脑 · 工作与语音来源";
  const network = {
    core_id: "local-check",
    core_epoch: "test",
    now: timestamp,
    agents: [
      {
        id: "agent-a",
        usage_fresh: true,
        codex_fresh: false,
        state: {
          agentId: "agent-a",
          hostLabel: computer,
          agentVersion: "test",
          agentEpoch: "test",
          metadata: { producedAt: timestamp },
        },
      },
      {
        id: "agent-b",
        usage_fresh: false,
        codex_fresh: false,
        state: {
          agentId: "agent-b",
          hostLabel: "备用电脑",
          agentVersion: "test",
          agentEpoch: "test",
          metadata: { producedAt: timestamp },
        },
      },
    ],
    nodes: [
      {
        nodeId: "screen-a",
        modelId: "xiaozhi-320x240",
        seriesId: "screen",
        variantId: "test",
        firmwareVersion: "test",
        nodeEpoch: "test",
        metadata: { producedAt: timestamp },
      },
    ],
    node_labels: { "screen-a": "书桌显示屏 · 较长名称也应完整换行" },
    archived_nodes: ["archived-phone"],
  };
  const routes = {
    revision: 1,
    routes: {
      "screen-a": {
        profile: "metrics-xiaozhi-320x240",
        display_mode: "usage",
        inputs: [{ agent_id: "agent-a", observation_type: "usage" }],
      },
    },
  };
  const devices = {
    enabled: true,
    devices: [
      {
        id: "phone-a",
        label: "验收手机",
        platform: "android",
        revoked: false,
        activity: { connections: 1, last_seen: timestamp },
      },
      {
        id: "archived-phone",
        label: "已归档的验收设备",
        platform: "web",
        revoked: false,
        activity: { connections: 0 },
      },
    ],
  };
  async function responses(target: Page) {
    await target.route("**/api/state", (route) =>
      route.fulfill({ json: network }),
    );
    await target.route("**/api/routes", (route) =>
      route.fulfill({ json: routes }),
    );
    await target.route("**/api/app/devices", (route) =>
      route.fulfill({ json: devices }),
    );
  }
  await responses(page);
  await login(page);
  await page.getByRole("link", { name: "总览", exact: true }).click();
  await expect(page.locator(".sources-table tbody tr")).toHaveCount(2);
  await expect(page.locator(".devices-table tbody tr")).toHaveCount(2);
  await expect(page.locator(".sources-table")).toContainText(computer);
  await expect(page.locator(".sources-table")).toContainText("无新鲜数据");
  await expect(page.locator(".devices-table")).toContainText("API 用量");
  await expect(page.locator(".devices-table")).toContainText("已连接");
  await expect(page.locator(".devices-table")).not.toContainText(
    "已归档的验收设备",
  );
  await expect(
    page
      .getByRole("navigation", { name: "工作", exact: true })
      .getByRole("link"),
  ).toHaveCount(4);
  await expect(
    page
      .getByRole("navigation", { name: "管理", exact: true })
      .getByRole("link"),
  ).toHaveCount(4);
  const cells = await page
    .locator(".devices-table tbody tr td:first-child")
    .evaluateAll((elements) =>
      elements.map((element) => element.getBoundingClientRect().x),
    );
  expect(new Set(cells).size).toBe(1);
  await page.screenshot({
    path: testInfo.outputPath("overview-desktop.png"),
    fullPage: true,
    animations: "disabled",
  });
  const mobile = await browser.newContext({
    storageState: await context.storageState(),
    viewport: { width: 390, height: 844 },
  });
  const phone = await mobile.newPage();
  try {
    await responses(phone);
    await phone.goto("/");
    await expect(phone.locator(".devices-table tbody tr")).toHaveCount(2);
    await expect
      .poll(() => phone.evaluate(() => document.documentElement.scrollWidth))
      .toBe(390);
    await expect(phone.locator(".devices-table")).toContainText(
      "书桌显示屏 · 较长名称也应完整换行",
    );
    await phone.screenshot({
      path: testInfo.outputPath("overview-mobile.png"),
      fullPage: true,
      animations: "disabled",
    });
  } finally {
    await mobile.close();
  }
});
