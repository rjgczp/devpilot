export interface HistoryItem {
  id: string;
  configuration_version: string;
  checked_at: string;
  finished_at: string | null;
  status:
    | "success"
    | "failure"
    | "unknown"
    | "running"
    | "canceled"
    | "expired";
  http_status: number | null;
  duration_ms: number | null;
  failure_code: string | null;
}
// Keep missing/unknown measurements as gaps, including genuine measured zeroes.
export function historyTrend(newest: HistoryItem[]) {
  const rows = [...newest].reverse();
  const measured = (r: HistoryItem) =>
    (r.status === "success" || r.status === "failure") &&
    r.duration_ms !== null &&
    Number.isFinite(r.duration_ms) &&
    r.duration_ms >= 0;
  const max = Math.max(1, ...rows.filter(measured).map((r) => r.duration_ms!));
  const segments: {
    id: string;
    at: string;
    ms: number;
    x: number;
    y: number;
  }[][] = [];
  let segment: (typeof segments)[number] = [];
  let count = 0;
  rows.forEach((row, i) => {
    if (!measured(row)) {
      if (segment.length) segments.push(segment);
      segment = [];
      return;
    }
    count++;
    segment.push({
      id: row.id,
      at: row.checked_at,
      ms: row.duration_ms!,
      x: rows.length === 1 ? 417 : 54 + (i * 726) / (rows.length - 1),
      y: 153 - (row.duration_ms! / max) * 128,
    });
  });
  if (segment.length) segments.push(segment);
  return { segments, max, count };
}
