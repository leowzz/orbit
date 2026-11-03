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

test("independent gateway preserves API authentication and asset boundaries", async ({
  page,
}) => {
  for (const path of ["/", "/box", "/nodes/example"]) {
    const response = await page.request.get(path);
    expect(response.status()).toBe(200);
    expect(response.headers()["cache-control"]).toBe("no-store");
    expect(response.headers()["content-security-policy"]).toContain(
      "frame-ancestors 'none'",
    );
  }
  for (const path of [
    "/assets/missing.js",
    "/assets/missing.js.gz",
    "/.vite/manifest.json",
    "/missing.svg",
  ]) {
    expect((await page.request.get(path)).status()).toBe(404);
  }
  expect((await page.request.get("http://127.0.0.1:17623/box")).status()).toBe(
    404,
  );
  expect((await page.request.get("/api/auth/session")).status()).toBe(401);
  // An API ending in a file extension must still reach the auth middleware.
  expect(
    (await page.request.get("/api/app/attachments/missing.jpg")).status(),
  ).toBe(401);
  const response = await page.request.post("/api/auth/login", {
    headers: { Origin: "http://127.0.0.1:17622" },
    data: { password: "local-check-only" },
  });
  expect(response.status()).toBe(200);
  expect(response.headers()["set-cookie"]).toContain("HttpOnly");
  expect(response.headers()["set-cookie"]).toContain("SameSite=Strict");
  expect((await page.request.get("/api/auth/session")).status()).toBe(200);
  expect(
    (
      await page.request.post("/api/auth/logout", {
        headers: { Origin: "https://evil.example" },
      })
    ).status(),
  ).toBe(403);
  expect(
    (
      await page.request.post("/api/auth/logout", {
        headers: { Origin: "http://127.0.0.1:17622" },
      })
    ).status(),
  ).toBe(200);
  expect((await page.request.get("/api/auth/session")).status()).toBe(401);
});

test("template sidebar collapses, persists and supports the keyboard", async ({
  page,
}) => {
  await login(page);
  const sidebar = page.locator('[data-slot="sidebar"][data-state]');
  await page.locator('[data-sidebar="trigger"]').click();
  await expect(sidebar).toHaveAttribute("data-state", "collapsed");
  await page.reload();
  await expect(page.locator("#sync-state")).toHaveText("已同步");
  await expect(sidebar).toHaveAttribute("data-state", "collapsed");
  await page.keyboard.press("ControlOrMeta+b");
  await expect(sidebar).toHaveAttribute("data-state", "expanded");
});

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
  await expect(page.getByRole("button", { name: "切换导航" })).toBeVisible();
  await expect(page.locator("#main-navigation")).toHaveCount(0);
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
  await page.getByRole("button", { name: "切换导航" }).click();
  await expect(
    page.getByRole("dialog", { name: "导航", exact: true }),
  ).toBeVisible();
  await page.getByRole("link", { name: "总览", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "工作台", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "切换到深色模式" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect(page.locator("#main-navigation")).toHaveCount(0);
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
  expect(decodedJS).toBeLessThan(450000);
  expect(encodedJS).toBeLessThan(150000);
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
