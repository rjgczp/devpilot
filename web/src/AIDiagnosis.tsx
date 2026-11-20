import { useEffect, useRef, useState } from "react";
import { aborted, api, errorText, type Monitor } from "./api";
import { Notice } from "./ui";

interface Diagnosis {
  summary: string;
  evidence: string[];
  possible_causes: string[];
  confidence: "low" | "medium" | "high";
  limitations: string[];
  recommendations: string[];
}

const confidence = { low: "低", medium: "中", high: "高" };

// Parent keys this component by monitor scope/version so results never survive
// a scope change. Model text is rendered as plain React text, never HTML/links.
export function AIDiagnosis({ monitor }: { monitor: Monitor }) {
  const [result, setResult] = useState<Diagnosis | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const active = useRef<AbortController | null>(null);
  useEffect(() => () => active.current?.abort(), []);

  function cancel() {
    active.current?.abort();
    active.current = null;
    setBusy(false);
    setError("已取消等待。服务端可能已开始模型调用，请稍后再试。");
  }

  async function diagnose() {
    if (active.current) return;
    const controller = new AbortController();
    active.current = controller;
    setBusy(true);
    setResult(null);
    setError("");
    let timedOut = false;
    const timeout = window.setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, 45000);
    try {
      const { body } = await api<{ data: Diagnosis }>("/api/v1/ai/diagnose", {
        method: "POST",
        body: JSON.stringify({
          workspace_id: monitor.workspace_id,
          project_id: monitor.project_id,
          monitor_id: monitor.id,
        }),
        signal: controller.signal,
      });
      if (active.current === controller && !controller.signal.aborted)
        setResult(body.data);
    } catch (e) {
      if (active.current === controller) {
        if (timedOut) setError("AI 诊断超时，请稍后手动重试。");
        else if (!aborted(e)) setError(errorText(e));
      }
    } finally {
      window.clearTimeout(timeout);
      if (active.current === controller) {
        active.current = null;
        setBusy(false);
      }
    }
  }

  return (
    <section className="monitor-panel ai-panel" aria-label="AI 诊断" aria-busy={busy}>
      <div className="table-toolbar">
        <strong>AI 故障诊断</strong>
        <div className="ai-actions">
          <button className="button" disabled={busy} onClick={() => void diagnose()}>
            {busy ? "正在诊断…" : "AI 诊断"}
          </button>
          {busy && <button className="button" onClick={cancel}>取消等待</button>}
        </div>
      </div>
      <div className="ai-content" aria-live="polite">
        <p className="field-help">
          点击后将此监控配置与最近检查结果发送至管理员配置的模型进行只读分析。建议需人工核实。
        </p>
        {busy && <p role="status">正在分析当前监控的已有记录，请稍候。</p>}
        {error && <Notice>{error}</Notice>}
        {result && (
          <div className="ai-result">
            <h3>故障分析</h3>
            <p>{result.summary}</p>
            <p>模型置信度：{confidence[result.confidence]}</p>
            <ResultList title="分析证据" items={result.evidence} />
            <ResultList title="可能原因" items={result.possible_causes} />
            <ResultList title="置信度限制" items={result.limitations} />
            <ResultList title="排查建议" items={result.recommendations} />
          </div>
        )}
      </div>
    </section>
  );
}

function ResultList({ title, items }: { title: string; items: string[] }) {
  return <div><h3>{title}</h3>{items.length ? <ul>{items.map((item, i) => <li key={i}>{item}</li>)}</ul> : <p className="muted">暂无内容</p>}</div>;
}
