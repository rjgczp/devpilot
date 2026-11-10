import { expect, test } from "@playwright/test";
const monitor = {
  id: "m1",
  workspace_id: "w1",
  project_id: "p1",
  name: "HTTP 历史服务",
  version: "2",
  interval_seconds: 60,
  timeout_seconds: 10,
  expected_status: 200,
};
const old = {
  run_id: "old-success",
  configuration_version: "2",
  activation_generation: "1",
  scheduler_epoch: "1",
  attempt_state: "completed",
  http_status: 200,
  success: true,
  duration_ms: 30,
};
const status = {
  workspace_id: "w1",
  project_id: "p1",
  monitor_id: "m1",
  configuration_version: "2",
  activation_generation: "1",
  scheduler_epoch: "1",
  desired_state: "enabled",
  authorization_state: "valid",
  effective_authorization: false,
  scheduling_state: "blocked",
  in_flight: false,
  blocked_reason: "scheduler_unavailable",
  current_observation: old,
  latest_completed_observation: old,
  latest_attempt: {
    ...old,
    run_id: "unknown-new",
    attempt_state: "unknown",
    success: null,
    http_status: null,
    duration_ms: null,
  },
};
const rows = Array.from({ length: 21 }, (_, i) => ({
  id: `run-${i}`,
  configuration_version: i > 19 ? "1" : "2",
  checked_at: new Date(Date.UTC(2026, 10, 5, 0, 30 - i)).toISOString(),
  finished_at: null,
  status: i === 0 ? "unknown" : i === 2 ? "failure" : "success",
  http_status: i === 0 ? null : i === 2 ? 503 : 200,
  duration_ms: i === 0 ? null : 20 + i,
  failure_code: i === 2 ? "unexpected_status" : null,
}));

test("history is read-only, paginates, filters and never replaces new unknown with old success", async ({
  page,
}) => {
  const historyQueries: URL[] = [];
  let fail = false;
  let failNextPage = true;
  await page.route("**/api/v1/**", async (route) => {
    expect(route.request().method()).toBe("GET");
    const url = new URL(route.request().url());
    const path = url.pathname;
    if (path.endsWith("/history")) {
      historyQueries.push(url);
      if (fail)
        return route.fulfill({
          status: 404,
          json: { error: { code: "monitor_not_found" } },
        });
      const state = url.searchParams.get("state");
      const after = url.searchParams.get("after");
      if (after && failNextPage) {
        failNextPage = false;
        return route.fulfill({ status: 503, json: { error: { code: "service_unavailable" } } });
      }
      const data =
        state === "failure"
          ? [rows[2]]
          : state === "running"
            ? []
            : after
              ? [rows[20]]
              : rows.slice(0, 20);
      return route.fulfill({
        json: {
          configuration_version: "2",
          data,
          pagination: {
            next_after:
              state === "all" && !after
                ? "opaque-cursor-with-fixed-window"
                : null,
          },
        },
      });
    }
    if (path.endsWith("/execution-status"))
      return route.fulfill({ json: { data: status } });
    if (path.endsWith("/auth/me"))
      return route.fulfill({
        json: { data: { id: "u1", email: "reader@example.com" } },
      });
    if (path.endsWith("/workspaces"))
      return route.fulfill({
        json: {
          data: [{ id: "w1", name: "只读 Workspace", role: "member" }],
          pagination: { next_after: null },
        },
      });
    if (path.endsWith("/projects"))
      return route.fulfill({
        json: {
          data: [{ id: "p1", workspace_id: "w1", name: "服务" }],
          pagination: { next_after: null },
        },
      });
    return route.fulfill({
      json: { data: [monitor], pagination: { next_after: null } },
    });
  });
  await page.goto("/");
  await page.getByRole("button", { name: "查看 HTTP 历史服务 历史" }).click();
  await expect(
    page.getByRole("heading", { name: "HTTP 历史服务" }),
  ).toBeVisible();
  await expect(page.getByLabel("当前执行状态")).toContainText("结果未知");
  await expect(page.getByLabel("检查历史")).toContainText("已加载 20 条");
  await expect(page.getByRole("img", { name: /响应时间趋势图/ })).toBeVisible();
  await expect(page.locator(".response-trend circle")).toHaveCount(19);
  await page.getByRole("button", { name: "加载更早记录" }).click();
  await expect(page.getByText(/历史读取失败/)).toBeVisible();
  await expect(page.getByLabel("检查历史")).toContainText("已加载 20 条");
  await expect(page.locator('.history-table tbody tr')).toHaveCount(20);
  await page.getByRole('button', { name: '重试读取历史' }).click();
  await expect(page.getByLabel("检查历史")).toContainText("已加载 21 条");
  expect(historyQueries.at(-1)!.searchParams.get("after")).toBe(
    "opaque-cursor-with-fixed-window",
  );
  await expect(page.getByText("配置 v1 · 历史配置")).toBeVisible();
  await page.getByLabel("历史状态筛选").selectOption("failure");
  await expect(page.getByLabel("检查历史")).toContainText("已加载 1 条");
  await expect(page.getByLabel("检查历史")).toContainText("HTTP 状态不符");
  await expect(page.getByLabel("当前执行状态")).toContainText("结果未知");
  expect(historyQueries.at(-1)?.searchParams.get("after")).toBeNull();
  await page.screenshot({
    path: "test-results/history-light.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: "切换深色模式" }).click();
  await page.screenshot({
    path: "test-results/history-dark.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  const durationCaption = await page.locator('.history-table td[data-label="响应时间"]').first().evaluate(el => getComputedStyle(el, '::before').content);
  expect(durationCaption).toContain('响应时间');
  expect(durationCaption).not.toContain('下一次到期');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({
    path: "test-results/history-mobile.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 320, height: 740 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: 'test-results/history-mobile-320.png', fullPage: true });
  await page.getByLabel("历史状态筛选").selectOption("running");
  await expect(
    page.getByRole("heading", { name: "没有符合筛选条件的检查" }),
  ).toBeVisible();
  await expect(page.getByRole("img", { name: /响应时间趋势图/ })).toHaveCount(
    0,
  );
  fail = true;
  await page.getByRole("button", { name: "刷新历史" }).click();
  await expect(page.getByText(/历史读取失败/)).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "没有符合筛选条件的检查" }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("row").filter({ hasText: "HTTP 状态不符" }),
  ).toHaveCount(0);
  expect(
    historyQueries.every((q) => q.searchParams.get("limit") === "20"),
  ).toBe(true);
});
