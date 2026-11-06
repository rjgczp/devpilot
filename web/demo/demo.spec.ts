import { expect, test } from "@playwright/test";
import { readFileSync, readdirSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";

test("real isolated Web Owner -> three60s Run/GET/Result/physical release cycles -> live history", async ({
  page,
}) => {
  const state = resolve(process.cwd(), "..", ".devpilot-isolated-demo");
  const credentialName = readdirSync(state).find((n) =>
    /^credentials-[0-9]+\.json$/.test(n),
  );
  if (!credentialName)
    throw new Error(
      "Start scripts/dev-isolated-demo.sh and wait for ready first",
    );
  const credentials = JSON.parse(
    readFileSync(resolve(state, credentialName), "utf8"),
  );
  const targets: string[] = [];
  page.on("request", (r) => {
    if (!r.url().startsWith("http://127.0.0.1:5174/")) targets.push(r.url());
  });
  await page.goto("/");
  const privateFile = resolve(state, credentialName);
  expect((await page.request.get("/@fs" + privateFile)).status()).toBe(403);
  await expect(
    page.getByRole("button", { name: "创建账号", exact: true }),
  ).toHaveCount(0);
  await expect(page.getByText(/临时 Owner 登录资料/)).toBeVisible();
  await page.getByLabel("邮箱", { exact: true }).fill(credentials.email);
  await page.getByLabel("密码", { exact: true }).fill(credentials.password);
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await expect(page.getByText("本地演示 · 无公网出口")).toBeVisible();
  const row = page
    .getByRole("row")
    .filter({ hasText: "本地演示 · 隔离 HTTPS fixture" });
  await expect(row).toContainText("未授权");
  await expect(row.getByRole("button", { name: /编辑/ })).toBeDisabled();
  const listing = await page.request.get("/api/v1/workspaces");
  const ws = (await listing.json()).data[0];
  const projects = await page.request.get(
    `/api/v1/workspaces/${ws.id}/projects`,
  );
  const project = (await projects.json()).data[0];
  await page.getByLabel("Workspace", { exact: true }).selectOption(ws.id);
  await page.getByLabel("Project", { exact: true }).selectOption(project.id);
  await expect(row).toBeVisible();
  const path = `/api/v1/workspaces/${ws.id}/projects/${project.id}/monitors`;
  const monitors = await page.request.get(path);
  const monitor = (await monitors.json()).data[0];
  const headers = {
    Origin: "http://127.0.0.1:5174",
    "X-DevPilot-CSRF": "1",
    "Content-Type": "application/json",
    "If-Match": `"monitor:${monitor.id}:${monitor.version}"`,
  };
  const before = await page.request.get(`${path}/${monitor.id}/history`);
  expect((await before.json()).data).toEqual([]);
  for (const url of [
    "https://example.com/",
    "http://127.0.0.1/",
    "https://a.fixture.example.com/other",
  ]) {
    expect(
      (
        await page.request.post(path, {
          headers,
          data: { name: "unapproved", url },
        })
      ).status(),
    ).toBe(403);
    expect(
      (
        await page.request.patch(`${path}/${monitor.id}`, {
          headers,
          data: { url },
        })
      ).status(),
    ).toBe(403);
  }
  await row.getByRole("button", { name: "授权激活", exact: true }).click();
  await expect(page.getByRole("dialog")).toContainText(
    "https://a.fixture.example.com/fixture/private-path-SENTINEL",
  );
  await expect(
    page.getByRole("button", { name: "确认授权", exact: true }),
  ).toBeDisabled();
  await page.getByRole("checkbox").check();
  await page.getByRole("button", { name: "确认授权", exact: true }).click();
  await expect(row).toContainText("授权有效");
  await row.getByRole("button", { name: /查看.*历史/ }).click();
  // Remain on this page: history and trend must update via real status polling,
  // without manual refresh, fixture rows or mocked browser API responses.
  await expect
    .poll(() => page.locator("tbody tr").count(), {
      timeout: 180000,
      intervals: [2000],
    })
    .toBe(3);
  const response = await page.request.get(`${path}/${monitor.id}/history`);
  const raw = await response.text();
  const history = JSON.parse(raw);
  expect(history.data).toHaveLength(3);
  expect(new Set(history.data.map((r: { id: string }) => r.id)).size).toBe(3);
  for (const r of history.data) {
    expect(r.status).toBe("success");
    expect(r.http_status).toBe(200);
    expect(r.duration_ms).toBeGreaterThanOrEqual(0);
    expect(r.duration_ms).toBeLessThanOrEqual(10000);
  }
  for (const key of [
    "termination_receipt",
    "namespace_incarnation",
    "fencing_token",
    "lease_owner",
    "password",
  ])
    expect(raw).not.toContain(key);
  const times = history.data.map((r: { checked_at: string }) =>
    Date.parse(r.checked_at),
  );
  expect(times[0] - times[1]).toBeGreaterThanOrEqual(60000);
  expect(times[1] - times[2]).toBeGreaterThanOrEqual(60000);
  await expect(page.locator(".response-trend circle")).toHaveCount(3);
  expect(targets).toEqual([]);
  await page.screenshot({
    path: "test-results/real-isolated-history.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: "← 返回监控" }).click();
  await row.getByRole("button", { name: "暂停", exact: true }).click();
  await expect(row).toContainText("意图已暂停");
  writeFileSync(
    resolve(
      state,
      credentialName.replace("credentials-", "stop-").replace(".json", ""),
    ),
    "test-complete\n",
  );
});
