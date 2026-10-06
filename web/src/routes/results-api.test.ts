import { describe, expect, it } from "vitest";
import { fillDays, period, validateResultsSearch } from "./results-api";

describe("results", () => {
  it("computes the period up to today, inclusive", () => {
    expect(period(7, new Date(2026, 9, 5, 23, 30))).toEqual({ from: "2026-09-29", to: "2026-10-05" });
    expect(period(30, new Date(2026, 2, 1))).toEqual({ from: "2026-01-31", to: "2026-03-01" });
  });

  it("fills the days without sales", () => {
    const days = fillDays("2026-10-01", "2026-10-03", [
      { day: "2026-10-02", orders: 2, estimated_commission_cents: 150, validated_commission_cents: 0 },
    ]);
    expect(days.map((d) => [d.day, d.orders])).toEqual([
      ["2026-10-01", 0],
      ["2026-10-02", 2],
      ["2026-10-03", 0],
    ]);
  });

  it("accepts only the offered periods", () => {
    expect(validateResultsSearch({ days: "7" })).toEqual({ days: 7 });
    expect(validateResultsSearch({ days: 30 })).toEqual({ days: undefined });
    expect(validateResultsSearch({ days: 15 })).toEqual({ days: undefined });
  });
});
