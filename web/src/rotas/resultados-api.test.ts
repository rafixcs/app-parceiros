import { describe, expect, it } from "vitest";
import { periodo, preencherDias, validarBuscaResultados } from "./resultados-api";

describe("resultados", () => {
  it("calcula o período até hoje, inclusive", () => {
    expect(periodo(7, new Date(2026, 9, 5, 23, 30))).toEqual({ de: "2026-09-29", ate: "2026-10-05" });
    expect(periodo(30, new Date(2026, 2, 1))).toEqual({ de: "2026-01-31", ate: "2026-03-01" });
  });

  it("completa os dias sem venda", () => {
    const dias = preencherDias("2026-10-01", "2026-10-03", [
      { dia: "2026-10-02", pedidos: 2, comissao_estimada_centavos: 150, comissao_validada_centavos: 0 },
    ]);
    expect(dias.map((d) => [d.dia, d.pedidos])).toEqual([
      ["2026-10-01", 0],
      ["2026-10-02", 2],
      ["2026-10-03", 0],
    ]);
  });

  it("aceita só os períodos oferecidos", () => {
    expect(validarBuscaResultados({ dias: "7" })).toEqual({ dias: 7 });
    expect(validarBuscaResultados({ dias: 30 })).toEqual({ dias: undefined });
    expect(validarBuscaResultados({ dias: 15 })).toEqual({ dias: undefined });
  });
});
