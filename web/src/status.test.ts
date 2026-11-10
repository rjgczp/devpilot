import { describe, expect, it } from "vitest";
import type { Attempt, ExecutionStatus, Monitor } from "./api";
import { executionSummary, presentStatus, validScope } from "./status";

const attempt: Attempt = {
  run_id: "run-1",
  configuration_version: "1",
  activation_generation: "2",
  scheduler_epoch: "1",
  attempt_state: "completed",
  http_status: 200,
  success: true,
  failure_code: null,
  duration_ms: 30,
  started_at: null,
  finished_at: null,
};
const base: ExecutionStatus = {
  workspace_id: "w1",
  project_id: "p1",
  monitor_id: "m1",
  configuration_version: "1",
  activation_generation: "2",
  scheduler_epoch: "1",
  desired_state: "enabled",
  authorization_state: "valid",
  effective_authorization: true,
  scheduling_state: "authorized_waiting",
  next_due: null,
  expires_at: null,
  in_flight: false,
  blocked_reason: null,
  latest_attempt: attempt,
  current_observation: attempt,
  latest_completed_observation: attempt,
};

describe("authoritative execution presentation", () => {
  it("separates paused intent from actual execution without inventing health", () => {
    const paused = {
      ...base,
      desired_state: "disabled",
      authorization_state: "none",
    };
    expect(executionSummary({ ...paused, latest_attempt: null }).label).toBe(
      "尚无检查",
    );
    expect(executionSummary(paused)).toMatchObject({
      label: "已完成",
      tone: "neutral",
    });
    expect(
      executionSummary({
        ...paused,
        latest_attempt: { ...attempt, attempt_state: "unknown" },
      }),
    ).toMatchObject({ label: "结果未知", tone: "warn" });
    expect(
      executionSummary({
        ...paused,
        latest_attempt: { ...attempt, attempt_state: "started" },
      }),
    ).toMatchObject({ label: "检查中", tone: "active" });
  });
  it("never masks a newer unknown with an old success", () => {
    const view = presentStatus({
      ...base,
      latest_attempt: {
        ...attempt,
        run_id: "run-2",
        attempt_state: "unknown",
        success: null,
        http_status: null,
      },
      in_flight: true,
    });
    expect(view.label).toBe("结果未知");
    expect(view.tone).toBe("warn");
    expect(view.detail).toContain("尚未确认释放");
  });
  it("keeps running/unknown visible after Pause or configuration revocation", () => {
    const old = {
      ...base,
      desired_state: "disabled",
      authorization_state: "none",
      activation_generation: "3",
      current_observation: null,
    };
    expect(
      presentStatus({
        ...old,
        latest_attempt: { ...attempt, attempt_state: "started" },
      }).label,
    ).toBe("旧代次 · 检查中");
    expect(
      presentStatus({
        ...old,
        latest_attempt: { ...attempt, attempt_state: "unknown" },
      }).label,
    ).toBe("旧代次 · 结果未知");
  });
  it("only reports health for the matching latest completed Run and lineage", () => {
    expect(presentStatus(base).label).toBe("已完成 · 正常");
    expect(
      presentStatus({
        ...base,
        current_observation: { ...attempt, run_id: "older-run" },
      }).label,
    ).toBe("已完成");
    expect(
      presentStatus({
        ...base,
        current_observation: { ...attempt, activation_generation: "1" },
      }).tone,
    ).toBe("neutral");
    expect(
      presentStatus({
        ...base,
        latest_attempt: { ...attempt, run_id: "latest" },
      }).tone,
    ).toBe("neutral");
  });
  it("separates authorization, unavailable reads and pre-execution gates from target failure", () => {
    expect(presentStatus(null).label).toBe("状态不可用");
    expect(
      presentStatus({
        ...base,
        latest_attempt: null,
        authorization_state: "none",
      }).label,
    ).toBe("未授权");
    expect(
      presentStatus({
        ...base,
        latest_attempt: null,
        authorization_state: "expired",
      }).label,
    ).toBe("授权已过期");
    expect(
      presentStatus({
        ...base,
        latest_attempt: null,
        effective_authorization: false,
        blocked_reason: "scheduler_unavailable",
      }).detail,
    ).toBe("执行环境未开放");
    expect(
      presentStatus({
        ...base,
        latest_attempt: { ...attempt, attempt_state: "leased" },
      }).label,
    ).toBe("等待执行");
  });
  it("rejects mixed scopes and a response from a changed configuration", () => {
    const m = {
      id: "m1",
      workspace_id: "w1",
      project_id: "p1",
      version: "1",
    } as Monitor;
    expect(validScope(m, base)).toBe(true);
    for (const change of [
      { monitor_id: "m2" },
      { workspace_id: "w2" },
      { project_id: "p2" },
      { configuration_version: "2" },
    ])
      expect(validScope(m, { ...base, ...change })).toBe(false);
  });
});
