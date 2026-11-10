import { isolatedDemo } from "./environment";
import { useEffect, useState } from "react";
import {
  aborted,
  api,
  errorText,
  type ExecutionStatus,
  type Monitor,
  type Page,
} from "./api";
import { formatTime, presentStatus, reasons } from "./status";
import { Notice } from "./ui";
import { historyTrend, type HistoryItem } from "./history";

type HistoryPage = Page<HistoryItem> & { configuration_version: string };
const labels: Record<string, string> = {
  success: "成功",
  failure: "失败",
  unknown: "结果未知",
  running: "检查中",
  canceled: "已取消",
  expired: "已过期",
};
const failures: Record<string, string> = {
  dns_failed: "域名解析失败",
  blocked_target: "目标未获准",
  connect_failed: "连接失败",
  tls_failed: "TLS 校验失败",
  timeout: "检查超时",
  canceled: "检查已取消",
  response_limit: "响应超出限制",
  protocol_error: "HTTP 协议错误",
  unexpected_status: "HTTP 状态不符",
};
export function MonitorHistory({
  monitor,
  path,
  status,
  close,
}: {
  monitor: Monitor;
  path: string;
  status?: ExecutionStatus;
  close: () => void;
}) {
  const [filter, setFilter] = useState("all");
  const [refresh, setRefresh] = useState(0);
  // A keyed child discards the previous filter/scope's data synchronously.
  return (
    <>
      <button className="button compact" onClick={close}>
        ← 返回监控
      </button>
      <div className="page-heading history-heading">
        <div>
          <span className="eyebrow">CHECK HISTORY</span>
          <h1>{monitor.name}</h1>
          <p>最近 HTTP 检查 · 只读历史记录</p>
        </div>
        <button className="button" onClick={() => setRefresh((x) => x + 1)}>
          刷新历史
        </button>
      </div>
      <div className="history-current" aria-label="当前执行状态">
        <span className={`badge ${presentStatus(status).tone}`}>
          {presentStatus(status).label}
        </span>
        <span className="muted">{presentStatus(status).detail}</span>
      </div>
      {status?.blocked_reason && (
        <Notice
          kind={
            status.blocked_reason === "scheduler_unavailable" ? "note" : "error"
          }
        >
          {reasons[status.blocked_reason] ?? "当前执行条件尚未满足"}
          {isolatedDemo && status.blocked_reason === "scheduler_unavailable"
            ? "。隔离演示当前不再开始新的检查；已有历史仍可查看。"
            : "。历史记录不代表当前执行环境已开放。"}
        </Notice>
      )}
      <section className="monitor-panel" aria-label="检查历史">
        <div className="table-toolbar">
          <strong>检查历史</strong>
          <label>
            状态筛选{" "}
            <select
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              aria-label="历史状态筛选"
            >
              <option value="all">全部状态</option>
              {Object.entries(labels).map(([key, label]) => (
                <option key={key} value={key}>
                  {label}
                </option>
              ))}
            </select>
          </label>
        </div>
        <HistoryRows
          key={`${path}/${monitor.id}/${filter}/${refresh}/${isolatedDemo ? `${status?.latest_attempt?.run_id}:${status?.latest_attempt?.attempt_state}:${status?.latest_attempt?.finished_at}` : ""}`}
          path={`${path}/${monitor.id}/history`}
          filter={filter}
        />
      </section>
    </>
  );
}
function HistoryRows({ path, filter }: { path: string; filter: string }) {
  const [page, setPage] = useState<HistoryPage | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(true);
  const [after, setAfter] = useState<string | null>(null);
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    let alive = true;
    setBusy(true);
    setError("");
    const query = new URLSearchParams({ limit: "20", state: filter });
    if (after) query.set("after", after);
    void api<HistoryPage>(`${path}?${query}`, { signal: controller.signal })
      .then(({ body }) => {
        if (alive)
          setPage((previous) => ({
            ...body,
            data:
              after && previous ? [...previous.data, ...body.data] : body.data,
          }));
      })
      .catch((e) => {
        if (alive && !aborted(e)) {
          if (!after) setPage(null);
          setError(errorText(e));
        }
      })
      .finally(() => {
        if (alive) setBusy(false);
      });
    return () => {
      alive = false;
      controller.abort();
    };
  }, [path, filter, after, retry]);
  const rows = page?.data ?? [];
  return (
    <div aria-busy={busy}>
      {error && (
        <Notice>
          历史读取失败：{error}{" "}
          <button
            className="text-button"
            disabled={busy}
            onClick={() => setRetry((x) => x + 1)}
          >
            重试读取历史
          </button>
          {rows.length ? " 已加载记录仍保留，尚未加载更多记录。" : ""}
        </Notice>
      )}
      {
        <>
          {!!rows.length && <ResponseTrend rows={rows} />}
          {!rows.length && !error && (
            <div className="empty-state">
              <h3>
                {busy
                  ? "正在读取检查历史…"
                  : filter === "all"
                    ? "尚无检查历史"
                    : "没有符合筛选条件的检查"}
              </h3>
              <p>
                {busy
                  ? "正在获取当前监控的真实记录，请稍候。"
                  : filter !== "all"
                    ? "可切换到全部状态，查看其他检查记录。"
                    : isolatedDemo
                      ? "请返回监控，由临时 Owner 确认目标并授权。只有真实检查开始后才会产生历史。"
                      : "普通管理环境不发起检查，暂无历史属于正常情况。已有历史只展示真实检查，授权本身不会生成结果。"}
              </p>
            </div>
          )}
          {!!rows.length && (
            <div className="table-scroll">
              <table className="history-table">
                <thead>
                  <tr>
                    <th>检查时间</th>
                    <th>结果</th>
                    <th>HTTP 状态</th>
                    <th>响应时间</th>
                    <th>说明</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((row) => (
                    <tr key={row.id}>
                      <td className="time-cell" data-label="检查时间">
                        {formatTime(row.checked_at)}
                        <small className="status-sub">
                          配置 v{row.configuration_version}
                          {row.configuration_version !==
                          page?.configuration_version
                            ? " · 历史配置"
                            : ""}
                        </small>
                      </td>
                      <td data-label="结果">
                        <span
                          className={`badge ${row.status === "success" ? "good" : row.status === "failure" ? "bad" : row.status === "unknown" ? "warn" : "neutral"}`}
                        >
                          {labels[row.status] ?? "状态不可用"}
                        </span>
                      </td>
                      <td className="mono" data-label="HTTP 状态">
                        {row.http_status ?? "—"}
                      </td>
                      <td className="mono" data-label="响应时间">
                        {row.duration_ms === null
                          ? "—"
                          : `${row.duration_ms} ms`}
                      </td>
                      <td className="muted" data-label="说明">
                        {row.status === "unknown"
                          ? "结果未确认，不自动重发"
                          : row.failure_code
                            ? (failures[row.failure_code] ?? "检查失败")
                            : row.status === "running"
                              ? "等待最小结果"
                              : "—"}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <div className="history-footer">
            <span className="muted">
              {busy && !page
                ? "正在读取…"
                : `已加载 ${rows.length} 条 · 每页 20 条`}
            </span>
            {!error && page?.pagination.next_after && (
              <button
                className="button"
                disabled={busy}
                onClick={() => setAfter(page.pagination.next_after)}
              >
                {busy ? "读取中…" : "加载更早记录"}
              </button>
            )}
          </div>
        </>
      }
    </div>
  );
}
function ResponseTrend({ rows }: { rows: HistoryItem[] }) {
  const { segments, max, count } = historyTrend(rows);
  return (
    <figure className="response-trend">
      <figcaption>
        <strong>响应时间</strong>
        <span className="muted"> {count} 次有耗时的检查 · 毫秒</span>
      </figcaption>
      {count ? (
        <svg
          role="img"
          aria-label="响应时间趋势图：未知结果不按零毫秒绘制"
          viewBox="0 0 800 190"
        >
          {[0, 1, 2].map((n) => (
            <g key={n}>
              <line
                x1="54"
                x2="780"
                y1={25 + n * 64}
                y2={25 + n * 64}
                stroke="var(--line)"
              />
              <text x="4" y={29 + n * 64} fill="var(--muted)" fontSize="12">
                {Math.round(max * (1 - n / 2))}
              </text>
            </g>
          ))}
          {segments.map((points, i) => (
            <g key={i}>
              <polyline
                points={points.map((p) => `${p.x},${p.y}`).join(" ")}
                stroke="var(--green)"
                strokeWidth="2"
                fill="none"
              />
              {points.map((p) => (
                <circle
                  key={p.id}
                  cx={p.x}
                  cy={p.y}
                  r="3.5"
                  fill="var(--green)"
                >
                  <title>
                    {formatTime(p.at)} · {p.ms} ms
                  </title>
                </circle>
              ))}
            </g>
          ))}
          <text x="54" y="182" fill="var(--muted)" fontSize="12">
            较早
          </text>
          <text x="750" y="182" fill="var(--muted)" fontSize="12">
            较新
          </text>
        </svg>
      ) : (
        <p className="muted">暂无可绘制的响应时间。</p>
      )}
      <p className="field-help">
        按已加载检查顺序展示；筛选仅影响历史。未知、运行中或无耗时的检查不按 0
        ms 计，缺失处断开连线。
      </p>
    </figure>
  );
}
