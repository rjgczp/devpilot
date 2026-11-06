export interface User {
  id: string;
  email: string;
}
export interface Workspace {
  id: string;
  name: string;
  role: "owner" | "member";
}
export interface Project {
  id: string;
  workspace_id: string;
  name: string;
  version: string;
}
export interface Monitor {
  id: string;
  workspace_id: string;
  project_id: string;
  name: string;
  version: string;
  url?: string;
  interval_seconds: number;
  timeout_seconds: number;
  expected_status: number;
}
export interface Page<T> {
  data: T[];
  pagination: { next_after: string | null };
}
export interface Activation {
  configuration_version: string;
  activation_generation: string;
  desired_state: string;
  next_enable_allowed_at: string | null;
}
export interface Attempt {
  run_id: string;
  configuration_version: string;
  activation_generation: string;
  scheduler_epoch: string;
  attempt_state: string;
  http_status: number | null;
  success: boolean | null;
  failure_code: string | null;
  duration_ms: number | null;
  started_at: string | null;
  finished_at: string | null;
}
export interface ExecutionStatus {
  workspace_id: string;
  project_id: string;
  monitor_id: string;
  configuration_version: string;
  activation_generation: string;
  scheduler_epoch: string | null;
  desired_state: string;
  authorization_state: string;
  effective_authorization: boolean;
  scheduling_state: string;
  next_due: string | null;
  expires_at: string | null;
  in_flight: boolean;
  blocked_reason: string | null;
  latest_attempt: Attempt | null;
  current_observation: Attempt | null;
  latest_completed_observation: Attempt | null;
}

const errors: Record<string, string> = {
  isolated_demo_readonly:
    "隔离演示只允许固定 Monitor，不能创建或修改目标配置。",
  invalid_credentials: "邮箱或密码不正确。",
  email_conflict: "该邮箱已注册，请登录。",
  unauthenticated: "登录已过期，请重新登录。",
  csrf_rejected: "请求来源不匹配，请从本次启动脚本提示的本机入口打开应用。",
  precondition_failed: "资源已被修改，请刷新后重新确认。",
  precondition_required: "缺少资源版本，请刷新后再操作。",
  monitor_forbidden: "只有 Workspace Owner 可以修改监控。",
  project_forbidden: "只有 Owner 可以创建 Project。",
  validation_failed: "请检查输入内容。",
  activation_rate_limited: "仍在授权冷却期，请稍后再试。",
  activation_limit_reached: "启用数量已达到 Workspace 上限。",
};
export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    public fields: Record<string, string> = {},
  ) {
    super(
      errors[code] ??
        (status >= 500 || status === 0
          ? "操作结果未确认，请刷新核对。不会自动重试。"
          : `请求未完成（${code}），请检查权限或刷新。`),
    );
  }
}

// Only same-origin API paths. Cookies stay HttpOnly; never store a token or
// fetch a Monitor's URL. Writes have no automatic retry, including unknown ACKs.
export async function api<T>(path: string, init: RequestInit = {}) {
  if (!path.startsWith("/api/v1/")) throw new Error("Invalid API path");
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  if (init.method && init.method !== "GET") {
    headers.set("Content-Type", "application/json");
    headers.set("X-DevPilot-CSRF", "1");
  }
  let response: Response;
  try {
    response = await fetch(path, {
      ...init,
      headers,
      credentials: "same-origin",
      cache: "no-store",
      redirect: "error",
    });
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError")
      throw error;
    throw new ApiError(0, "unconfirmed");
  }
  let body;
  try {
    body = response.status === 204 ? undefined : await response.json();
  } catch {
    throw new ApiError(response.status, "invalid_response");
  }
  if (!response.ok) {
    if (response.status === 401 && body.error?.code !== "invalid_credentials")
      window.dispatchEvent(new Event("session-expired"));
    throw new ApiError(
      response.status,
      body.error?.code ?? "request_failed",
      body.error?.fields,
    );
  }
  return {
    body: body as T,
    etag: response.headers.get("ETag"),
    projection: response.headers.get("DevPilot-Activation-Projection"),
  };
}
export const scopePath = (wid: string, pid: string) =>
  `/api/v1/workspaces/${encodeURIComponent(wid)}/projects/${encodeURIComponent(pid)}/monitors`;
export function strongTag(tag: string | null) {
  if (!tag || !tag.startsWith('"') || !tag.endsWith('"'))
    throw new ApiError(0, "missing_version");
  return tag;
}
export const aborted = (error: unknown) =>
  error instanceof DOMException && error.name === "AbortError";
export function errorText(error: unknown) {
  if (error instanceof ApiError && Object.keys(error.fields).length) {
    const fields: Record<string, string> = {
      email: "邮箱",
      password: "密码",
      url: "目标 URL",
      name: "名称",
      interval_seconds: "检查间隔",
      timeout_seconds: "超时",
      expected_status: "预期状态码",
    };
    return `${error.message}（${Object.keys(error.fields)
      .map((key) => fields[key] ?? key)
      .join("、")}）`;
  }
  return error instanceof Error
    ? error.message
    : "暂时无法完成，请刷新后核对。";
}
