import { useEffect, useState, type FormEvent } from "react";
import {
  aborted,
  api,
  errorText,
  scopePath,
  strongTag,
  type Activation,
  type Monitor,
} from "./api";
import { Dialog, Notice } from "./ui";
import { formatTime } from "./status";

export function MonitorForm({
  wid,
  pid,
  monitor,
  close,
  saved,
}: {
  wid: string;
  pid: string;
  monitor: Monitor | null;
  close: () => void;
  saved: (message: string) => void;
}) {
  const [input, setInput] = useState({
    name: "",
    url: "",
    interval_seconds: 60,
    timeout_seconds: 10,
    expected_status: 200,
  });
  const [tag, setTag] = useState<string | null>(null);
  const [loading, setLoading] = useState(!!monitor);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    if (!monitor) return;
    const controller = new AbortController();
    setLoading(true);
    setError("");
    setTag(null);
    api<{ data: Monitor }>(`${scopePath(wid, pid)}/${monitor.id}`, {
      signal: controller.signal,
    })
      .then(({ body, etag }) => {
        if (controller.signal.aborted) return;
        const m = body.data;
        setInput({
          name: m.name,
          url: m.url ?? "",
          interval_seconds: m.interval_seconds,
          timeout_seconds: m.timeout_seconds,
          expected_status: m.expected_status,
        });
        setTag(strongTag(etag));
        setLoading(false);
      })
      .catch((e) => {
        if (!aborted(e) && !controller.signal.aborted) {
          setError(errorText(e));
          setLoading(false);
        }
      });
    return () => controller.abort();
  }, [wid, pid, monitor, revision]);
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      await api(`${scopePath(wid, pid)}${monitor ? `/${monitor.id}` : ""}`, {
        method: monitor ? "PATCH" : "POST",
        headers: monitor ? { "If-Match": strongTag(tag) } : {},
        body: JSON.stringify(input),
      });
      saved(
        monitor
          ? "监控已保存。原授权已撤销，如需继续检查，请重新授权。"
          : "监控已创建。确认目标后可授予有限期执行授权。",
      );
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog title={monitor ? "编辑监控" : "创建监控"} close={close} busy={busy}>
      <p className="muted">
        使用 HTTP GET 检查响应状态。保存配置本身不会发送请求。
      </p>
      {error && (
        <Notice>
          {error}{" "}
          {monitor && (
            <button
              type="button"
              className="text-button"
              onClick={() => setRevision((x) => x + 1)}
            >
              重新加载配置
            </button>
          )}
        </Notice>
      )}
      {loading ? (
        <p className="loading">正在读取当前配置…</p>
      ) : (
        <form onSubmit={submit}>
          <label>
            监控名称
            <input
              autoFocus
              required
              maxLength={100}
              value={input.name}
              onChange={(e) => setInput({ ...input, name: e.target.value })}
              placeholder="例如：支付服务 API"
            />
          </label>
          <label>
            目标 URL
            <input
              type="url"
              required
              maxLength={2048}
              value={input.url}
              onChange={(e) => setInput({ ...input, url: e.target.value })}
              placeholder="https://api.example.com/health"
            />
          </label>
          <p className="field-help">
            支持 HTTP(S) 域名与默认端口；配置校验不代表目标已被访问或可达。
          </p>
          <div className="form-grid">
            <label>
              检查间隔
              <select
                value={input.interval_seconds}
                onChange={(e) =>
                  setInput({ ...input, interval_seconds: +e.target.value })
                }
              >
                {[
                  [60, "60 秒"],
                  [300, "5 分钟"],
                  [900, "15 分钟"],
                  [1800, "30 分钟"],
                  [3600, "1 小时"],
                ].map(([value, label]) => (
                  <option value={value} key={value}>
                    {label}
                  </option>
                ))}
              </select>
            </label>
            <label>
              超时（秒）
              <input
                type="number"
                min="1"
                max="30"
                required
                value={input.timeout_seconds}
                onChange={(e) =>
                  setInput({ ...input, timeout_seconds: +e.target.value })
                }
              />
            </label>
            <label>
              预期状态码
              <input
                type="number"
                min="200"
                max="599"
                required
                value={input.expected_status}
                onChange={(e) =>
                  setInput({ ...input, expected_status: +e.target.value })
                }
              />
            </label>
          </div>
          {monitor && (
            <p className="field-help">
              任何编辑都会撤销已有授权；在途检查仍按原版本记录结果。
            </p>
          )}
          <footer className="dialog-actions">
            <button
              type="button"
              className="button"
              onClick={close}
              disabled={busy}
            >
              取消
            </button>
            <button
              className="button primary"
              disabled={busy || (!!monitor && !tag)}
            >
              {busy ? "正在保存…" : "保存监控"}
            </button>
          </footer>
        </form>
      )}
    </Dialog>
  );
}

export function AuthorizeForm({
  monitor,
  close,
  saved,
}: {
  monitor: Monitor;
  close: () => void;
  saved: (message: string) => void;
}) {
  const path = `${scopePath(monitor.workspace_id, monitor.project_id)}/${monitor.id}`;
  const [snapshot, setSnapshot] = useState<{
    tag: string;
    activation: Activation;
    url: string;
  } | null>(null);
  const [seconds, setSeconds] = useState(900);
  const [consent, setConsent] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    Promise.all([
      api<{ data: Activation }>(`${path}/activation`, {
        signal: controller.signal,
      }),
      api<{ data: Monitor }>(path, { signal: controller.signal }),
    ])
      .then(([a, m]) => {
        if (controller.signal.aborted) return;
        if (
          a.projection !== "intent-v2" ||
          a.body.data.configuration_version !== m.body.data.version ||
          m.body.data.version !== monitor.version
        )
          throw new Error("配置或服务版本已变化，请刷新列表后重新确认。");
        setSnapshot({
          tag: strongTag(a.etag),
          activation: a.body.data,
          url: m.body.data.url ?? "",
        });
      })
      .catch((e) => {
        if (!aborted(e) && !controller.signal.aborted) setError(errorText(e));
      });
    return () => controller.abort();
  }, [path, monitor.version]);
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!snapshot || !consent || busy) return;
    setBusy(true);
    setError("");
    try {
      await api(`${path}/execution-authorization`, {
        method: "PUT",
        headers: { "If-Match": snapshot.tag },
        body: JSON.stringify({
          configuration_version: snapshot.activation.configuration_version,
          consent_profile: "periodic-http-get-v1",
          authorization_seconds: seconds,
        }),
      });
      saved("有限期授权已确认。是否实际执行，以当前执行状态和环境门控为准。");
    } catch (e) {
      setError(errorText(e));
      setConsent(false);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog title="授权激活" close={close} busy={busy}>
      <p className="muted">
        仅当前 Workspace Owner 可以确认。授权到期后不会自动续期。
      </p>
      {error && <Notice>{error}</Notice>}
      {!snapshot ? (
        <p className="loading">
          {error ? "请关闭并刷新后重试。" : "正在读取目标与当前授权版本…"}
        </p>
      ) : (
        <form onSubmit={submit}>
          <div className="target-summary">
            <strong>{monitor.name}</strong>
            <span className="mono">{snapshot.url}</span>
            <span>GET · 每 {monitor.interval_seconds} 秒</span>
          </div>
          <label>
            本次授权时长
            <select
              value={seconds}
              onChange={(e) => setSeconds(+e.target.value)}
            >
              <option value="900">15 分钟</option>
              <option value="3600">1 小时</option>
              <option value="86400">24 小时</option>
            </select>
          </label>
          {snapshot.activation.next_enable_allowed_at && (
            <p className="field-help">
              已有冷却限制仍适用：
              {formatTime(snapshot.activation.next_enable_allowed_at)}
            </p>
          )}
          <label className="consent">
            <input
              type="checkbox"
              checked={consent}
              onChange={(e) => setConsent(e.target.checked)}
              required
            />
            <span>
              我确认以上目标与检查间隔，并授权本次有限期周期 HTTP GET 检查。
            </span>
          </label>
          <Notice kind="info">
            本地普通环境保持执行出口关闭。授权不会绕过隔离、停止或资源门控。
          </Notice>
          <footer className="dialog-actions">
            <button
              type="button"
              className="button"
              onClick={close}
              disabled={busy}
            >
              取消
            </button>
            <button className="button primary" disabled={!consent || busy}>
              {busy ? "正在确认…" : "确认授权"}
            </button>
          </footer>
        </form>
      )}
    </Dialog>
  );
}
