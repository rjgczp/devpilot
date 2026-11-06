import { afterEach, expect, it, vi } from "vitest";
import { api, strongTag } from "./api";
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
it("uses only same-origin cookies, CSRF and the exact captured ETag for writes", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(
      new Response('{"data":{}}', {
        status: 200,
        headers: { ETag: '"monitor:m1:2"' },
      }),
    );
  vi.stubGlobal("fetch", fetcher);
  const result = await api("/api/v1/workspaces/w1/projects/p1/monitors/m1", {
    method: "PATCH",
    body: '{"name":"new"}',
    headers: { "If-Match": '"monitor:m1:1"' },
  });
  const init = fetcher.mock.calls[0][1];
  expect(init.credentials).toBe("same-origin");
  expect(init.redirect).toBe("error");
  expect(init.headers.get("X-DevPilot-CSRF")).toBe("1");
  expect(init.headers.get("If-Match")).toBe('"monitor:m1:1"');
  expect(init.headers.has("Authorization")).toBe(false);
  expect(result.etag).toBe('"monitor:m1:2"');
  await expect(api("https://monitor.example.com")).rejects.toThrow(
    "Invalid API path",
  );
  expect(fetcher).toHaveBeenCalledTimes(1);
});
it("does not retry a 503/unknown mutation or a stale-version write", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(
      new Response('{"error":{"code":"service_unavailable"}}', { status: 503 }),
    );
  vi.stubGlobal("fetch", fetcher);
  await expect(
    api("/api/v1/workspaces", { method: "POST", body: '{"name":"new"}' }),
  ).rejects.toThrow("结果未确认");
  expect(fetcher).toHaveBeenCalledTimes(1);
  fetcher.mockResolvedValue(
    new Response('{"error":{"code":"precondition_failed"}}', { status: 412 }),
  );
  await expect(
    api("/api/v1/workspaces/w1/projects/p1/monitors/m1", {
      method: "PATCH",
      body: "{}",
    }),
  ).rejects.toThrow("重新确认");
  expect(fetcher).toHaveBeenCalledTimes(2);
  expect(() => strongTag('W/"weak"')).toThrow();
});
it("expires the UI session on401 without persisting or synthesizing a token", async () => {
  const dispatchEvent = vi.fn();
  vi.stubGlobal("window", { dispatchEvent });
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response('{"error":{"code":"unauthenticated"}}', { status: 401 }),
      ),
  );
  await expect(api("/api/v1/auth/me")).rejects.toThrow("重新登录");
  expect(dispatchEvent).toHaveBeenCalledTimes(1);
});
it("accepts the existing204 logout response with no JSON body", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(new Response(null, { status: 204 })),
  );
  await expect(
    api("/api/v1/auth/logout", { method: "POST", body: "{}" }),
  ).resolves.toMatchObject({ body: undefined });
});
