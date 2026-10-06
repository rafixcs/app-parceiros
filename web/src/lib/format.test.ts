import { describe, expect, it } from "vitest";
import { count, duration, fileSize, money, percent, priceRange, timeAgo, toCents } from "./format";

const nbsp = (s: string) => s.replace(/ /g, " ");

describe("format", () => {
  it("formats money in reais", () => {
    expect(nbsp(money(12990))).toBe("R$ 129,90");
    expect(nbsp(priceRange(1990, 1990))).toBe("R$ 19,90");
    expect(nbsp(priceRange(1990, 2990))).toBe("R$ 19,90 – R$ 29,90");
  });

  it("formats commission in basis points", () => {
    expect(percent(1200)).toBe("12%");
    expect(percent(1250)).toBe("12,5%");
  });

  it("abbreviates large counts", () => {
    expect(count(950)).toBe("950");
    expect(nbsp(count(15300))).toBe("15,3 mil");
  });

  it("parses text into cents", () => {
    expect(toCents("R$ 12,90")).toBe(1290);
    expect(toCents("1.299,00")).toBe(129900);
    expect(toCents("")).toBeUndefined();
    expect(toCents("abc")).toBeUndefined();
  });

  it("says how long ago", () => {
    const now = new Date("2026-10-02T12:00:00Z");
    expect(timeAgo("2026-10-02T11:59:30Z", now)).toBe("agora há pouco");
    expect(timeAgo("2026-10-02T11:15:00Z", now)).toBe("há 45 min");
    expect(timeAgo("2026-10-02T06:00:00Z", now)).toBe("há 6 h");
    expect(timeAgo("2026-09-30T12:00:00Z", now)).toBe("há 2 dias");
  });

  it("formats file size", () => {
    expect(fileSize(900)).toBe("900 bytes");
    expect(fileSize(1536)).toBe("1,5 KB");
    expect(fileSize(5 * 1024 ** 3)).toBe("5 GB");
  });

  it("formats video duration", () => {
    expect(duration(7)).toBe("0:07");
    expect(duration(65)).toBe("1:05");
    expect(duration(3725)).toBe("1:02:05");
  });
});
