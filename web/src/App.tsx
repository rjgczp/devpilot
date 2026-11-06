import { useEffect, useState, type FormEvent } from "react";
import {
  aborted,
  api,
  errorText,
  scopePath,
  strongTag,
  type Activation,
  type ExecutionStatus,
  type Monitor,
  type Project,
  type User,
  type Workspace,
} from "./api";
import { isolatedDemo } from "./environment";
import { MonitorHistory } from "./MonitorHistory";
import { AuthorizeForm, MonitorForm } from "./MonitorForm";
import { formatTime, presentStatus, validScope } from "./status";
import { Dialog, Icon, Notice } from "./ui";
import { usePage } from "./usePage";

export default function App() {
  const [user, setUser] = useState<User | null>(null);
  const [checking, setChecking] = useState(true);
  const [initialError, setInitialError] = useState("");
  const [theme, setTheme] = useState(() => {
    try {
      return (
        localStorage.getItem("devpilot-theme") ??
        (matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light")
      );
    } catch {
      return "light";
    }
  });
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    try {
      localStorage.setItem("devpilot-theme", theme);
    } catch {
      /* Nonessential preference. */
    }
  }, [theme]);
  useEffect(() => {
    const controller = new AbortController();
    const expired = () => setUser(null);
    window.addEventListener("session-expired", expired);
    api<{ data: User }>("/api/v1/auth/me", { signal: controller.signal })
      .then(({ body }) => {
        if (!controller.signal.aborted) setUser(body.data);
      })
      .catch((e) => {
        if (!aborted(e) && !controller.signal.aborted && e.status !== 401)
          setInitialError(errorText(e));
      })
      .finally(() => {
        if (!controller.signal.aborted) setChecking(false);
      });
    return () => {
      controller.abort();
      window.removeEventListener("session-expired", expired);
    };
  }, []);
  const themeButton = (
    <button
      className="icon-button"
      aria-label={theme === "dark" ? "切换浅色模式" : "切换深色模式"}
      onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
    >
      <Icon name={theme === "dark" ? "sun" : "moon"} />
    </button>
  );
  if (checking)
    return (
      <div className="app-loading">
        <Brand />
        <p>正在恢复登录状态…</p>
      </div>
    );
  return user ? (
    <WorkspaceApp
      key={user.id}
      user={user}
      logout={() => setUser(null)}
      themeButton={themeButton}
    />
  ) : (
    <AuthView
      error={initialError}
      loggedIn={setUser}
      themeButton={themeButton}
    />
  );
}

function Brand() {
  return (
    <div className="brand">
      <span className="brand-mark">
        <Icon name="pulse" size={24} />
      </span>
      <span>
        DevPilot<span className="brand-ai">AI</span>
      </span>
    </div>
  );
}

function AuthView({
  loggedIn,
  themeButton,
  error: initialError,
}: {
  loggedIn: (user: User) => void;
  themeButton: React.ReactNode;
  error: string;
}) {
  const [mode, setMode] = useState<"login" | "register">("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(initialError);
  const [message, setMessage] = useState("");
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    setMessage("");
    try {
      const result = await api<{ data: User }>(`/api/v1/auth/${mode}`, {
        method: "POST",
        body: JSON.stringify({ email, password }),
      });
      setPassword("");
      if (mode === "register") {
        setMode("login");
        setMessage("账号已创建，请使用刚才的密码登录。");
      } else loggedIn(result.body.data);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="auth-layout">
      <div className="auth-art">
        <Brand />
        <div className="auth-story">
          <span className="eyebrow">YOUR SERVICES, IN FOCUS</span>
          <h1>
            让每一次检查，
            <br />
            都有清晰的答案。
          </h1>
          <p>
            集中管理 HTTP 监控，查看真实的执行状态。
            <br />
            从一个服务开始，保持简单。
          </p>
          <div className="signal-line">
            <i />
            <i />
            <i />
            <i />
            <i />
            <i />
            <i />
            <i />
            <i />
            <i />
            <i />
            <i />
          </div>
        </div>
        <span className="auth-footnote">HTTP MONITORING · LOCAL WORKSPACE</span>
      </div>
      <div className="auth-panel">
        <div className="auth-theme">{themeButton}</div>
        <main className="auth-form">
          <span className="eyebrow">DEV PILOT CONSOLE</span>
          <h2>{mode === "login" ? "欢迎回来" : "创建你的账号"}</h2>
          <p className="muted">
            {mode === "login"
              ? "登录以管理 Workspace 与监控。"
              : "注册后即可创建你的第一个 Workspace。"}
          </p>
          {error && <Notice>{error}</Notice>}
          {message && <Notice kind="info">{message}</Notice>}
          <form onSubmit={submit}>
            <label>
              邮箱
              <input
                autoComplete="username"
                type="email"
                required
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="you@example.com"
              />
            </label>
            <label>
              密码
              <input
                type="password"
                autoComplete={
                  mode === "login" ? "current-password" : "new-password"
                }
                required
                minLength={mode === "register" ? 15 : 1}
                maxLength={128}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
              />
            </label>
            {mode === "register" && (
              <p className="field-help">15–128 个字符，不裁剪或归一化密码。</p>
            )}
            <button className="button primary full" disabled={busy}>
              {busy ? "正在处理…" : mode === "login" ? "登录" : "注册账号"}
              <Icon name="arrow" size={16} />
            </button>
          </form>
          {isolatedDemo ? (
            <p className="field-help">
              本地隔离演示：请使用启动终端提示的临时 Owner
              登录资料。账号、Workspace、Project 和固定 Monitor
              已准备好，无需注册。
            </p>
          ) : (
            <p className="auth-switch">
              {mode === "login" ? "还没有账号？" : "已有账号？"}
              <button
                type="button"
                className="text-button"
                disabled={busy}
                onClick={() => {
                  setMode(mode === "login" ? "register" : "login");
                  setError("");
                  setMessage("");
                  setPassword("");
                }}
              >
                {mode === "login" ? "创建账号" : "返回登录"}
              </button>
            </p>
          )}
          <p className="auth-security">
            使用安全 Cookie 会话 · 凭据不会写入浏览器存储
          </p>
        </main>
      </div>
    </div>
  );
}

function WorkspaceApp({
  user,
  logout,
  themeButton,
}: {
  user: User;
  logout: () => void;
  themeButton: React.ReactNode;
}) {
  const [refresh, setRefresh] = useState(0);
  const [wid, setWid] = useState("");
  const [pid, setPid] = useState("");
  const [creating, setCreating] = useState<"workspace" | "project" | null>(
    null,
  );
  const [error, setError] = useState("");
  const [loggingOut, setLoggingOut] = useState(false);
  const workspaces = usePage<Workspace>("/api/v1/workspaces", user.id, refresh);
  const workspace = workspaces.items.find((w) => w.id === wid);
  const projects = usePage<Project>(
    workspace ? `/api/v1/workspaces/${workspace.id}/projects` : null,
    user.id,
    refresh,
  );
  const project = projects.items.find((p) => p.id === pid);
  useEffect(() => {
    if (workspaces.items.length && !workspaces.loading && !workspace)
      setWid(workspaces.items[0].id);
  }, [workspaces.items, workspaces.loading, workspace]);
  useEffect(() => {
    if (projects.items.length && !projects.loading && !project)
      setPid(projects.items[0].id);
  }, [projects.items, projects.loading, project]);
  async function signOut() {
    setLoggingOut(true);
    setError("");
    try {
      await api("/api/v1/auth/logout", { method: "POST", body: "{}" });
      logout();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setLoggingOut(false);
    }
  }
  return (
    <div className="shell">
      <aside className="sidebar">
        <Brand />
        <div className="workspace-picker">
          <label>
            WORKSPACE
            <select
              aria-label="Workspace"
              value={workspace?.id ?? ""}
              onChange={(e) => {
                setWid(e.target.value);
                setPid("");
                setCreating(null);
              }}
            >
              <option value="" disabled>
                选择 Workspace
              </option>
              {workspaces.items.map((w) => (
                <option key={w.id} value={w.id}>
                  {w.name}
                </option>
              ))}
            </select>
          </label>
          {workspaces.next && (
            <button
              className="text-button"
              onClick={workspaces.more}
              disabled={workspaces.loading}
            >
              加载更多 Workspace
            </button>
          )}
          <button
            className="text-button small"
            disabled={isolatedDemo}
            onClick={() => setCreating("workspace")}
          >
            <Icon name="plus" size={14} />
            新建 Workspace
          </button>
        </div>
        <span className="nav-caption">工作台</span>
        <div className="nav-item selected">
          <Icon name="grid" />
          HTTP 监控
          <span className="nav-dot" />
        </div>
        <div className="sidebar-note">
          <span className="small-dot" />
          <div>
            <strong>{isolatedDemo ? "隔离演示" : "本地开发环境"}</strong>
            <p>生产公网执行保持关闭</p>
          </div>
        </div>
        <div className="account">
          <span className="avatar">{user.email.slice(0, 1).toUpperCase()}</span>
          <div>
            <strong title={user.email}>{user.email}</strong>
            <span>
              {workspace?.role === "owner"
                ? "Workspace Owner"
                : "Workspace Member"}
            </span>
          </div>
          <button
            className="icon-button"
            onClick={signOut}
            disabled={loggingOut}
            aria-label="退出登录"
          >
            <Icon name="logout" size={17} />
          </button>
        </div>
      </aside>
      <div className="main-column">
        <header className="topbar">
          <span>
            {workspace?.name ?? "Workspace"}
            <Icon name="arrow" size={14} />
            <strong>HTTP 监控</strong>
          </span>
          {themeButton}
        </header>
        <main className="content">
          {isolatedDemo && (
            <div className="demo-banner" role="status">
              <strong>本地演示 · 无公网出口</strong>
              <p>
                仅固定 HTTPS fixture，配置只读。请以本次临时 Owner
                登录并授权激活；60秒周期，最多3次真实检查后关闭执行。暂停会停止新的批准，已开始的检查仍可能完成。
              </p>
            </div>
          )}
          {error && <Notice>{error}</Notice>}
          {workspaces.error && (
            <Notice>
              {workspaces.error}
              <button
                className="text-button"
                onClick={() => setRefresh((x) => x + 1)}
              >
                重试读取
              </button>
            </Notice>
          )}
          {projects.error && <Notice>{projects.error}</Notice>}
          <div className="project-bar">
            <label>
              PROJECT
              <select
                aria-label="Project"
                value={project?.id ?? ""}
                onChange={(e) => setPid(e.target.value)}
              >
                <option disabled value="">
                  选择 Project
                </option>
                {projects.items.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </label>
            {projects.next && (
              <button
                className="text-button"
                onClick={projects.more}
                disabled={projects.loading}
              >
                加载更多
              </button>
            )}
            {workspace?.role === "owner" && (
              <button
                className="text-button"
                disabled={isolatedDemo}
                onClick={() => setCreating("project")}
              >
                <Icon name="plus" size={14} />
                新建 Project
              </button>
            )}
          </div>
          {workspace && project ? (
            <Dashboard
              key={`${user.id}:${workspace.id}:${project.id}`}
              workspace={workspace}
              project={project}
              identity={user.id}
            />
          ) : (
            <section className="empty onboarding">
              <span className="empty-icon">
                <Icon name="pulse" size={32} />
              </span>
              <h1>
                {workspaces.loading || projects.loading
                  ? "正在加载工作台…"
                  : workspace
                    ? "为服务创建一个 Project"
                    : "你的监控工作台，从这里开始"}
              </h1>
              <p>
                {workspace
                  ? "Project 用于组织相关的服务与 HTTP 监控。"
                  : "创建 Workspace，然后添加 Project 与第一个监控。"}
              </p>
              {!workspaces.loading &&
                !projects.loading &&
                (!workspace || workspace.role === "owner") && (
                  <button
                    className="button primary"
                    onClick={() =>
                      setCreating(workspace ? "project" : "workspace")
                    }
                  >
                    <Icon name="plus" />
                    {workspace ? "创建 Project" : "创建 Workspace"}
                  </button>
                )}
            </section>
          )}
        </main>
        <footer className="app-footer">
          DevPilot AI<span>只展示已确认的执行事实</span>
        </footer>
      </div>
      {creating && (
        <NameForm
          key={`${creating}:${wid}`}
          kind={creating}
          wid={wid}
          close={() => setCreating(null)}
          saved={(created) => {
            if (creating === "workspace") {
              setWid(created.id);
              setPid("");
            } else setPid(created.id);
            setCreating(null);
            setRefresh((x) => x + 1);
          }}
        />
      )}
    </div>
  );
}

function NameForm({
  kind,
  wid,
  close,
  saved,
}: {
  kind: "workspace" | "project";
  wid: string;
  close: () => void;
  saved: (created: { id: string }) => void;
}) {
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const { body } = await api<{ data: { id: string } }>(
        kind === "workspace"
          ? "/api/v1/workspaces"
          : `/api/v1/workspaces/${wid}/projects`,
        { method: "POST", body: JSON.stringify({ name }) },
      );
      saved(body.data);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog
      title={kind === "workspace" ? "创建 Workspace" : "创建 Project"}
      close={close}
      busy={busy}
    >
      <form onSubmit={submit}>
        {error && <Notice>{error}</Notice>}
        <label>
          名称
          <input
            autoFocus
            required
            maxLength={100}
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={
              kind === "workspace" ? "例如：产品团队" : "例如：核心服务"
            }
          />
        </label>
        <footer className="dialog-actions">
          <button
            type="button"
            className="button"
            onClick={close}
            disabled={busy}
          >
            取消
          </button>
          <button className="button primary" disabled={busy}>
            {busy ? "正在创建…" : "创建"}
          </button>
        </footer>
      </form>
    </Dialog>
  );
}

function useStatuses(monitors: Monitor[], path: string) {
  const [values, setValues] = useState<
    Record<string, { value?: ExecutionStatus; error?: string; at: string }>
  >({});
  const signature = monitors.map((m) => `${m.id}:${m.version}`).join(",");
  useEffect(() => {
    const controller = new AbortController();
    let running = false;
    setValues({});
    async function poll() {
      if (running || document.hidden) return;
      running = true;
      let cursor = 0;
      async function worker() {
        while (cursor < monitors.length && !controller.signal.aborted) {
          const m = monitors[cursor++];
          try {
            const { body } = await api<{ data: ExecutionStatus }>(
              `${path}/${m.id}/execution-status`,
              { signal: controller.signal },
            );
            if (!validScope(m, body.data))
              throw new Error("配置已变化，请刷新列表。");
            if (!controller.signal.aborted)
              setValues((old) => ({
                ...old,
                [m.id]: { value: body.data, at: new Date().toISOString() },
              }));
          } catch (e) {
            if (!aborted(e) && !controller.signal.aborted)
              setValues((old) => ({
                ...old,
                [m.id]: { error: errorText(e), at: new Date().toISOString() },
              }));
          }
        }
      }
      await Promise.all(
        Array.from({ length: Math.min(4, monitors.length) }, worker),
      );
      running = false;
    }
    void poll();
    const timer = window.setInterval(() => void poll(), 10000);
    document.addEventListener("visibilitychange", poll);
    return () => {
      controller.abort();
      clearInterval(timer);
      document.removeEventListener("visibilitychange", poll);
    };
    // The signature contains exactly the scope/version inputs for this poll.
    // eslint is not required; no client cache merges observations across scopes.
  }, [signature, path]);
  return values;
}

function Dashboard({
  workspace,
  project,
  identity,
}: {
  workspace: Workspace;
  project: Project;
  identity: string;
}) {
  const path = scopePath(workspace.id, project.id);
  const [refresh, setRefresh] = useState(0);
  const monitors = usePage<Monitor>(path, identity, refresh);
  const statuses = useStatuses(monitors.items, path);
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState("all");
  const [modal, setModal] = useState<{
    kind: "new" | "edit" | "authorize";
    monitor: Monitor | null;
  } | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [historyID, setHistoryID] = useState<string | null>(null);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const owner = workspace.role === "owner";
  const editable = owner && !isolatedDemo;
  const saved = (text: string) => {
    setModal(null);
    setMessage(text);
    setError("");
    setRefresh((x) => x + 1);
  };
  async function pause(m: Monitor) {
    if (busy) return;
    setBusy(m.id);
    setError("");
    setMessage("");
    try {
      const { body, etag, projection } = await api<{ data: Activation }>(
        `${path}/${m.id}/activation`,
      );
      if (projection !== "intent-v2")
        throw new Error("当前服务未提供新版授权协议，请检查后端版本。");
      await api(`${path}/${m.id}/activation`, {
        method: "PUT",
        headers: { "If-Match": strongTag(etag) },
        body: JSON.stringify({
          desired_state: "disabled",
          configuration_version: body.data.configuration_version,
        }),
      });
      saved("已暂停，不再批准新的检查；已在途的检查仍可能完成。");
    } catch (e) {
      setError(errorText(e));
      setRefresh((x) => x + 1);
    } finally {
      setBusy("");
    }
  }
  const items = monitors.items.filter((m) => {
    if (!m.name.toLocaleLowerCase().includes(query.toLocaleLowerCase()))
      return false;
    const status = statuses[m.id]?.value;
    return (
      filter === "all" ||
      (filter === "unauthorized"
        ? status?.authorization_state === "none"
        : status?.latest_attempt?.attempt_state === filter)
    );
  });
  const active = Object.values(statuses).filter(
    (s) => s.value?.latest_attempt?.attempt_state === "started",
  ).length;
  const unknown = Object.values(statuses).filter(
    (s) => s.value?.latest_attempt?.attempt_state === "unknown",
  ).length;
  const completed = Object.values(statuses).filter(
    (s) => s.value?.latest_attempt?.attempt_state === "completed",
  ).length;
  const selectedMonitor = monitors.items.find((m) => m.id === selected);
  const historyMonitor = monitors.items.find((m) => m.id === historyID);
  if (historyMonitor)
    return (
      <MonitorHistory
        key={historyMonitor.id}
        monitor={historyMonitor}
        path={path}
        status={statuses[historyMonitor.id]?.value}
        close={() => setHistoryID(null)}
      />
    );
  return (
    <>
      <div className="page-heading">
        <div>
          <span className="eyebrow">MONITORING</span>
          <h1>
            HTTP 监控
            <span className="heading-count">{monitors.items.length}</span>
          </h1>
          <p>管理服务检查，掌握每一次执行的真实状态。</p>
        </div>
        <button
          className="button primary"
          disabled={!editable}
          onClick={() => setModal({ kind: "new", monitor: null })}
        >
          <Icon name="plus" />
          创建监控
        </button>
      </div>
      <section className="stats" aria-label="监控概览">
        {[
          ["已加载监控", monitors.items.length, "neutral"],
          ["正在检查", active, "active"],
          ["结果未知", unknown, "warn"],
          ["最近已完成", completed, "good"],
        ].map(([label, value, tone]) => (
          <div className="stat" key={label}>
            <span>
              <i className={`dot ${tone}`} />
              {label}
            </span>
            <strong>{value}</strong>
          </div>
        ))}
      </section>
      {message && <Notice kind="info">{message}</Notice>}
      {error && <Notice>{error}</Notice>}
      {monitors.error && <Notice>{monitors.error}</Notice>}
      {!owner && (
        <Notice kind="info">
          当前为 Member，只读查看监控与状态。创建、编辑和授权由 Owner 操作。
        </Notice>
      )}
      <section className="monitor-panel">
        <div className="table-toolbar">
          <div>
            <strong>服务监控</strong>
            <span className="live-label">
              <i className="small-dot" />每 10 秒刷新状态
            </span>
          </div>
          <div className="table-controls">
            <input
              aria-label="搜索监控"
              placeholder="搜索监控名称…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
            <select
              aria-label="筛选状态"
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
            >
              <option value="all">所有状态</option>
              <option value="started">检查中</option>
              <option value="unknown">结果未知</option>
              <option value="completed">已完成</option>
              <option value="unauthorized">未授权</option>
            </select>
            <button
              className="icon-button"
              aria-label="刷新监控"
              onClick={() => setRefresh((x) => x + 1)}
              disabled={monitors.loading}
            >
              <Icon name="refresh" />
            </button>
          </div>
        </div>
        {monitors.loading && !monitors.items.length ? (
          <p className="loading">正在读取监控…</p>
        ) : items.length ? (
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>监控</th>
                  <th>执行状态</th>
                  <th>检查间隔</th>
                  <th>下一次到期</th>
                  <th className="align-right">操作</th>
                </tr>
              </thead>
              <tbody>
                {items.map((m) => {
                  const s = statuses[m.id];
                  const view = presentStatus(s?.value);
                  return (
                    <tr key={m.id}>
                      <td>
                        <button
                          className="monitor-name"
                          onClick={() => setSelected(m.id)}
                        >
                          <span className={`service-icon ${view.tone}`}>
                            <Icon name="pulse" />
                          </span>
                          <span>
                            <strong>{m.name}</strong>
                            <small>HTTP GET · 预期 {m.expected_status}</small>
                          </span>
                        </button>
                      </td>
                      <td>
                        <span className={`badge ${view.tone}`}>
                          <i className="dot" />
                          {s ? view.label : "正在读取"}
                        </span>
                        <small className="status-sub">
                          {s?.value?.authorization_state === "valid"
                            ? "授权有效"
                            : s?.value?.authorization_state === "expired"
                              ? "授权已过期"
                              : s?.value
                                ? "未授权"
                                : s?.error
                                  ? "读取失败"
                                  : "—"}
                          {s?.value?.desired_state === "disabled"
                            ? " · 意图已暂停"
                            : ""}
                        </small>
                      </td>
                      <td className="mono">
                        {m.interval_seconds < 60
                          ? `${m.interval_seconds} 秒`
                          : `${m.interval_seconds / 60} 分钟`}
                      </td>
                      <td className="time-cell">
                        {formatTime(s?.value?.next_due)}
                      </td>
                      <td className="align-right">
                        <div className="row-actions">
                          <button
                            className="button compact"
                            aria-label={`查看 ${m.name} 历史`}
                            onClick={() => setHistoryID(m.id)}
                          >
                            历史
                          </button>
                          <button
                            className="button compact"
                            aria-label={`编辑 ${m.name}`}
                            disabled={!editable || !!busy}
                            onClick={() =>
                              setModal({ kind: "edit", monitor: m })
                            }
                          >
                            <Icon name="edit" size={14} />
                            <span>编辑</span>
                          </button>
                          <button
                            className="button compact"
                            disabled={!owner || !!busy}
                            onClick={() =>
                              setModal({ kind: "authorize", monitor: m })
                            }
                          >
                            授权激活
                          </button>
                          <button
                            className="button compact"
                            disabled={!owner || !!busy}
                            onClick={() => void pause(m)}
                          >
                            {busy === m.id ? "处理中…" : "暂停"}
                          </button>
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="empty">
            <span className="empty-icon">
              <Icon name="pulse" size={28} />
            </span>
            <h2>
              {query || filter !== "all"
                ? "没有匹配的监控"
                : "从第一个监控开始"}
            </h2>
            <p>
              {query || filter !== "all"
                ? "尝试其他名称或状态筛选。"
                : "添加服务地址，配置检查间隔，再确认执行授权。"}
            </p>
            {owner && !query && filter === "all" && (
              <button
                className="button"
                onClick={() => setModal({ kind: "new", monitor: null })}
              >
                <Icon name="plus" size={16} />
                创建监控
              </button>
            )}
          </div>
        )}
        <footer className="table-footer">
          <span>
            {project.name} · 已加载 {monitors.items.length} 项
            {monitors.next ? "，还有更多" : ""}
          </span>
          {monitors.next && (
            <button
              className="text-button"
              onClick={monitors.more}
              disabled={monitors.loading}
            >
              加载更多监控
            </button>
          )}
        </footer>
      </section>
      <p className="execution-note">
        有限期授权不等于已执行。暂停后不再开始新的检查，已在途或结果未知的检查仍会如实展示。
      </p>
      {modal &&
        (modal.kind === "authorize" && modal.monitor ? (
          <AuthorizeForm
            monitor={modal.monitor}
            close={() => setModal(null)}
            saved={saved}
          />
        ) : (
          <MonitorForm
            wid={workspace.id}
            pid={project.id}
            monitor={modal.monitor}
            close={() => setModal(null)}
            saved={saved}
          />
        ))}
      {selectedMonitor && (
        <StatusDetail
          monitor={selectedMonitor}
          record={statuses[selectedMonitor.id]}
          history={() => {
            setHistoryID(selectedMonitor.id);
            setSelected(null);
          }}
          close={() => setSelected(null)}
        />
      )}
    </>
  );
}

function StatusDetail({
  history,
  monitor,
  record,
  close,
}: {
  monitor: Monitor;
  record?: { value?: ExecutionStatus; error?: string; at: string };
  history: () => void;
  close: () => void;
}) {
  const s = record?.value;
  const view = presentStatus(s);
  const a = s?.latest_attempt;
  return (
    <Dialog title="监控执行状态" close={close}>
      <h3>{monitor.name}</h3>
      <span className={`badge ${view.tone}`}>
        <i className="dot" />
        {view.label}
      </span>
      <p className="muted">{view.detail}</p>
      {record?.error && <Notice>{record.error}</Notice>}
      <dl className="status-details">
        <div>
          <dt>授权状态</dt>
          <dd>
            {!s
              ? "不可用"
              : s.authorization_state === "valid"
                ? "有限期授权有效"
                : s.authorization_state === "expired"
                  ? "已过期"
                  : "未授权"}
          </dd>
        </div>
        <div>
          <dt>当前意图</dt>
          <dd>{s ? (s.desired_state === "enabled" ? "启用" : "暂停") : "—"}</dd>
        </div>
        <div>
          <dt>调度条件</dt>
          <dd>
            {s?.blocked_reason
              ? presentStatus({
                  ...s,
                  latest_attempt: null,
                  current_observation: null,
                }).detail
              : s?.scheduling_state === "due"
                ? "已到期"
                : s?.effective_authorization
                  ? "等待周期"
                  : "未开放"}
          </dd>
        </div>
        <div>
          <dt>授权到期</dt>
          <dd>{formatTime(s?.expires_at)}</dd>
        </div>
        <div>
          <dt>下一次到期</dt>
          <dd>{formatTime(s?.next_due)}</dd>
        </div>
        <div>
          <dt>物理执行</dt>
          <dd>
            {s
              ? s.in_flight
                ? "仍有在途或未确认释放的检查"
                : "无未释放执行"
              : "不可用"}
          </dd>
        </div>
        <div>
          <dt>最近尝试</dt>
          <dd>{a?.attempt_state ?? "尚无检查"}</dd>
        </div>
        <div>
          <dt>最近尝试时间</dt>
          <dd>{formatTime(a?.finished_at ?? a?.started_at)}</dd>
        </div>
        <div>
          <dt>最近 HTTP 状态</dt>
          <dd>
            {a?.attempt_state === "completed"
              ? (a.http_status ?? "无 HTTP 响应")
              : "—"}
          </dd>
        </div>
        <div>
          <dt>最近耗时</dt>
          <dd>
            {a?.attempt_state === "completed" && a.duration_ms !== null
              ? `${a.duration_ms} ms`
              : "—"}
          </dd>
        </div>
        <div>
          <dt>最近读取</dt>
          <dd>{formatTime(record?.at)}</dd>
        </div>
      </dl>
      {a && (
        <p className="field-help">
          最近尝试属于其原始配置与授权，不会用旧结果推断当前目标健康。可通过历史记录查看每一次检查。
        </p>
      )}
      <footer className="dialog-actions">
        <button className="button primary" onClick={history}>
          查看历史
        </button>
        <button className="button" onClick={close}>
          关闭
        </button>
      </footer>
    </Dialog>
  );
}
