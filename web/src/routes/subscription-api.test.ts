import { describe, expect, it } from "vitest";
import { accessStatusLabel, daysUntil, type Subscription } from "./subscription-api";

const base: Subscription = {
  plan: "mentorship",
  access_status: "trial",
  access_until: "2026-10-12T12:00:00Z",
  status: "none",
  provider: "",
  price_cents: 1490,
  seats: 0,
  seats_in_use: 0,
  max_seats: 200,
  amount_cents: 0,
  next_due_date: null,
  payment_url: null,
  simulated: false,
};

const now = new Date("2026-10-05T12:00:00Z");

describe("daysUntil", () => {
  it("counts the days left and the days already past", () => {
    expect(daysUntil("2026-10-12T12:00:00Z", now)).toBe(7);
    expect(daysUntil("2026-10-05T18:00:00Z", now)).toBe(1);
    expect(daysUntil("2026-10-04T12:00:00Z", now)).toBe(-1);
  });
});

describe("accessStatusLabel", () => {
  it("shows the trial days left", () => {
    expect(accessStatusLabel({ ...base, access_until: "2026-10-09T12:00:00Z" })).toMatch(/^Teste · \d+ dias restantes$/);
  });

  it("warns when the trial is ending", () => {
    const end = new Date(Date.now() + 3600_000).toISOString();
    expect(accessStatusLabel({ ...base, access_until: end })).toBe("Teste terminando");
  });

  it("names the active subscription and the suspended workspace", () => {
    expect(accessStatusLabel({ ...base, access_status: "active", status: "active" })).toBe("Assinatura ativa");
    expect(accessStatusLabel({ ...base, access_status: "suspended" })).toBe("Workspace suspenso");
    expect(accessStatusLabel({ ...base, access_status: "free" })).toBe("Grátis para alunos de mentoria");
  });
});
