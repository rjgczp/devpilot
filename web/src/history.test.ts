import { describe, expect, it } from "vitest";
import { historyTrend, type HistoryItem } from "./history";
const item = (
  id: string,
  status: HistoryItem["status"],
  duration_ms: number | null,
): HistoryItem => ({
  id,
  status,
  duration_ms,
  configuration_version: "1",
  checked_at: "2026-11-05T00:00:00Z",
  finished_at: null,
  http_status: null,
  failure_code: null,
});
describe("history measurement gaps", () => {
  it("keeps newer unknown from becoming an invented zero and breaks its line", () => {
    const trend = historyTrend([
      item("new-unknown", "unknown", null),
      item("failure", "failure", 80),
      item("running", "running", null),
      item("old-success", "success", 0),
    ]);
    expect(trend.count).toBe(2);
    expect(trend.max).toBe(80);
    expect(trend.segments.map((s) => s.map((p) => p.id))).toEqual([
      ["old-success"],
      ["failure"],
    ]);
    expect(trend.segments[0][0].ms).toBe(0);
    expect(trend.segments[0][0].x).toBeLessThan(trend.segments[1][0].x);
  });
  it("does not fabricate samples for empty, missing, invalid or unfinished results", () => {
    expect(historyTrend([]).count).toBe(0);
    expect(
      historyTrend([
        item("unknown", "unknown", 100),
        item("null", "success", null),
        item("invalid", "failure", NaN),
        item("negative", "success", -1),
      ]).segments,
    ).toEqual([]);
    expect(historyTrend([item("zero", "success", 0)]).segments[0][0].x).toBe(
      417,
    );
  });
});
