import { describe, expect, it } from "vitest";
import { diasAte, rotuloSituacao, type Assinatura } from "./assinatura-api";

const base: Assinatura = {
  plano: "mentoria",
  situacao: "teste",
  acesso_ate: "2026-10-12T12:00:00Z",
  status: "sem_assinatura",
  provedor: "",
  preco_centavos: 1490,
  assentos: 0,
  assentos_em_uso: 0,
  assentos_maximo: 200,
  valor_centavos: 0,
  proximo_ciclo: null,
  url_pagamento: null,
  simulavel: false,
};

const agora = new Date("2026-10-05T12:00:00Z");

describe("diasAte", () => {
  it("conta os dias que faltam e os que já passaram", () => {
    expect(diasAte("2026-10-12T12:00:00Z", agora)).toBe(7);
    expect(diasAte("2026-10-05T18:00:00Z", agora)).toBe(1);
    expect(diasAte("2026-10-04T12:00:00Z", agora)).toBe(-1);
  });
});

describe("rotuloSituacao", () => {
  it("mostra os dias restantes do teste", () => {
    expect(rotuloSituacao({ ...base, acesso_ate: "2026-10-09T12:00:00Z" })).toMatch(/^Teste · \d+ dias restantes$/);
  });

  it("avisa quando o teste está no fim", () => {
    const fim = new Date(Date.now() + 3600_000).toISOString();
    expect(rotuloSituacao({ ...base, acesso_ate: fim })).toBe("Teste terminando");
  });

  it("nomeia a assinatura ativa e o workspace suspenso", () => {
    expect(rotuloSituacao({ ...base, situacao: "ativo", status: "ativa" })).toBe("Assinatura ativa");
    expect(rotuloSituacao({ ...base, situacao: "suspenso" })).toBe("Workspace suspenso");
    expect(rotuloSituacao({ ...base, situacao: "gratuito" })).toBe("Grátis para alunos de mentoria");
  });
});
