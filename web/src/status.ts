import type { Attempt, ExecutionStatus, Monitor } from "./api";
export type Tone = "neutral" | "good" | "warn" | "bad" | "active";
export const reasons: Record<string, string> = {
  scheduler_unavailable: "执行环境未开放",
  egress_unavailable: "隔离出口未就绪",
  authorization_required: "需要 Owner 授权",
  authorization_expired: "授权已过期",
  owner_unavailable: "暂无有效 Owner",
  emergency_stop: "运维停止中",
  resource_limited: "等待执行资源",
  retention_capacity: "存储准入已关闭",
  quarantined: "等待可信物理释放",
  clock_untrusted: "时间校验未通过",
};
export function currentAttempt(s: ExecutionStatus, a: Attempt | null) {
  return (
    !!a &&
    a.configuration_version === s.configuration_version &&
    a.activation_generation === s.activation_generation &&
    a.scheduler_epoch === s.scheduler_epoch
  );
}
export function validScope(m: Monitor, s: ExecutionStatus) {
  return (
    m.id === s.monitor_id &&
    m.workspace_id === s.workspace_id &&
    m.project_id === s.project_id &&
    m.version === s.configuration_version
  );
}
export function presentStatus(s?: ExecutionStatus | null): {
  label: string;
  tone: Tone;
  detail: string;
} {
  if (!s)
    return {
      label: "状态不可用",
      tone: "neutral",
      detail: "无法读取权威状态，不推测是否正在执行。",
    };
  const a = s.latest_attempt;
  const old = a && !currentAttempt(s, a) ? "旧代次 · " : "";
  // Most recent attempt always wins over an earlier successful observation.
  if (a?.attempt_state === "unknown")
    return {
      label: `${old}结果未知`,
      tone: "warn",
      detail: s.in_flight
        ? "远端结果无法确认；物理执行尚未确认释放。"
        : "该次结果无法确认，系统不会重发该次 GET。",
    };
  if (a?.attempt_state === "started")
    return {
      label: `${old}检查中`,
      tone: "active",
      detail: "已批准的在途检查可能在暂停后继续完成。",
    };
  if (a?.attempt_state === "leased")
    return {
      label: `${old}等待执行`,
      tone: "neutral",
      detail: "任务已领取，尚未开始网络检查。",
    };
  if (s.desired_state === "disabled")
    return { label: "已暂停", tone: "neutral", detail: "不会批准新的检查。" };
  if (s.authorization_state === "none")
    return {
      label: "未授权",
      tone: "neutral",
      detail: "需要 Owner 明确确认有限期执行授权。",
    };
  if (s.authorization_state === "expired")
    return {
      label: "授权已过期",
      tone: "warn",
      detail: "续期需要 Owner 再次确认。",
    };
  if (a?.attempt_state === "completed" && currentAttempt(s, a)) {
    const o = s.current_observation;
    if (currentAttempt(s, o) && o?.run_id === a.run_id && o.success !== null)
      return {
        label: o.success ? "已完成 · 正常" : "已完成 · 异常",
        tone: o.success ? "good" : "bad",
        detail: o.http_status
          ? `HTTP ${o.http_status}`
          : (o.failure_code ?? "检查已结束"),
      };
    return {
      label: "已完成",
      tone: "neutral",
      detail: "当前观察尚不可用，不推断健康状态。",
    };
  }
  if (s.blocked_reason)
    return {
      label: "执行已阻断",
      tone: "warn",
      detail: reasons[s.blocked_reason] ?? "执行条件尚未满足",
    };
  if (!s.effective_authorization)
    return {
      label: "执行未就绪",
      tone: "neutral",
      detail: "授权意图不等于已经开始执行。",
    };
  return {
    label: s.scheduling_state === "due" ? "已到期" : "等待下个周期",
    tone: "neutral",
    detail: "授权有效，等待调度。",
  };
}
export const formatTime = (value?: string | null) =>
  value ? new Date(value).toLocaleString("zh-CN", { hour12: false }) : "—";

// The list shows execution facts separately from enable/authorization intent.
// Keep the authoritative newest-attempt priority and never infer old health.
export function executionSummary(s?: ExecutionStatus) {
  const view = presentStatus(s);
  if (!["已暂停", "未授权", "授权已过期"].includes(view.label)) return view;
  if (!s?.latest_attempt) return { ...view, label: "尚无检查" };
  const terminal: Record<string, string> = {
    completed: "已完成",
    canceled: "已取消",
    expired: "已过期",
  };
  const label = terminal[s.latest_attempt.attempt_state];
  return label
    ? {
        label,
        tone: "neutral" as const,
        detail: "最近一次检查已结束，启用与授权状态单独显示。",
      }
    : view;
}
