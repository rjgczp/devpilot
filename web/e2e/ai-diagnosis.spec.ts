import { expect, test, type Page } from "@playwright/test";

const monitors = ["m1", "m2"].map((id) => ({
  id, workspace_id: "w1", project_id: "p1", name: `服务 ${id}`,
  version: "1", interval_seconds: 60, timeout_seconds: 10, expected_status: 200,
}));
const diagnosis = {
  summary: '<img src=x onerror="window.aiInjected=true">',
  evidence: ["最近 2 次检查返回 503"], possible_causes: ["上游服务暂时不可用"],
  confidence: "low", limitations: ["仅依据历史记录，无法确认实时状态"],
  recommendations: ["检查服务端日志并核对部署时间"],
};

async function mockDashboard(page: Page) {
  await page.route("**/api/v1/**", async (route) => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith("/auth/me")) return route.fulfill({ json: { data: { id: "u1", email: "member@example.com" } } });
    if (path.endsWith("/workspaces")) return route.fulfill({ json: { data: [{ id: "w1", name: "Workspace", role: "member" }], pagination: { next_after: null } } });
    if (path.endsWith("/projects")) return route.fulfill({ json: { data: [{ id: "p1", workspace_id: "w1", name: "服务" }], pagination: { next_after: null } } });
    if (path.endsWith("/history")) return route.fulfill({ json: { configuration_version: "1", data: [], pagination: { next_after: null } } });
    if (path.endsWith("/execution-status")) return route.fulfill({ status: 503, json: { error: { code: "service_unavailable" } } });
    return route.fulfill({ json: { data: monitors, pagination: { next_after: null } } });
  });
  await page.goto("/");
  await page.getByRole("button", { name: "查看 服务 m1 历史" }).click();
}

test("member explicitly requests scoped diagnosis with CSRF; renders plain text and clears on monitor change", async ({ page }) => {
  await mockDashboard(page);
  const calls: unknown[] = [];
  await page.route("**/api/v1/ai/diagnose", async (route) => {
    expect(route.request().method()).toBe("POST");
    expect(route.request().headers()["x-devpilot-csrf"]).toBe("1");
    expect(route.request().headers()["authorization"]).toBeUndefined();
    calls.push(route.request().postDataJSON());
    return route.fulfill({ json: { data: diagnosis } });
  });
  expect(calls).toHaveLength(0);
  await page.getByRole("button", { name: "AI 诊断", exact: true }).click();
  const panel = page.getByLabel("AI 诊断", { exact: true });
  await expect(panel).toContainText(diagnosis.summary);
  await expect(panel).toContainText("模型置信度：低");
  for (const text of ["分析证据", "可能原因", "置信度限制", "排查建议", diagnosis.limitations[0]]) await expect(panel).toContainText(text);
  await expect(panel.locator("img")).toHaveCount(0);
  expect(await page.evaluate(() => "aiInjected" in window)).toBe(false);
  expect(calls).toEqual([{ workspace_id: "w1", project_id: "p1", monitor_id: "m1" }]);
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.getByRole("button", { name: "← 返回监控" }).click();
  await page.getByRole("button", { name: "查看 服务 m2 历史" }).click();
  await expect(panel).not.toContainText(diagnosis.summary);
  expect(calls).toHaveLength(1);
});

test("shows provider failures and timeout without automatic retry; pending calls can be canceled", async ({ page }) => {
  await mockDashboard(page);
  let calls = 0;
  await page.route("**/api/v1/ai/diagnose", async (route) => {
    calls++;
    return route.fulfill({ status: calls === 1 ? 502 : 504, json: { error: { code: calls === 1 ? "ai_provider_failed" : "ai_timeout" } } });
  });
  await page.getByRole("button", { name: "AI 诊断", exact: true }).click();
  await expect(page.getByLabel("AI 诊断", { exact: true })).toContainText("模型服务暂时不可用");
  expect(calls).toBe(1);
  await page.getByRole("button", { name: "AI 诊断", exact: true }).click();
  await expect(page.getByLabel("AI 诊断", { exact: true })).toContainText("AI 诊断超时");
  expect(calls).toBe(2);
  let release!: () => void;
  await page.route("**/api/v1/ai/diagnose", async (route) => {
    calls++;
    await new Promise<void>((resolve) => { release = resolve; });
    await route.fulfill({ json: { data: diagnosis } }).catch(() => {});
  });
  await page.getByRole("button", { name: "AI 诊断", exact: true }).click();
  await expect(page.getByRole("button", { name: "正在诊断…" })).toBeDisabled();
  await expect(page.getByRole("status")).toContainText("正在分析");
  await expect.poll(() => calls).toBe(3);
  await page.getByRole("button", { name: "取消等待" }).click();
  release();
  await expect(page.getByLabel("AI 诊断", { exact: true })).toContainText("已取消等待");
  await expect(page.getByLabel("AI 诊断", { exact: true })).not.toContainText(diagnosis.summary);
  await expect(page.getByRole("button", { name: "AI 诊断", exact: true })).toBeEnabled();
  expect(calls).toBe(3);
});
