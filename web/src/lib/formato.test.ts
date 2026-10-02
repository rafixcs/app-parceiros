import { describe, expect, it } from "vitest";
import { duracao, faixaPreco, haQuanto, paraCentavos, porcentagem, quantidade, reais, tamanho } from "./formato";

const nbsp = (s: string) => s.replace(/ /g, " ");

describe("formato", () => {
  it("formata dinheiro em reais", () => {
    expect(nbsp(reais(12990))).toBe("R$ 129,90");
    expect(nbsp(faixaPreco(1990, 1990))).toBe("R$ 19,90");
    expect(nbsp(faixaPreco(1990, 2990))).toBe("R$ 19,90 – R$ 29,90");
  });

  it("formata comissão em basis points", () => {
    expect(porcentagem(1200)).toBe("12%");
    expect(porcentagem(1250)).toBe("12,5%");
  });

  it("abrevia quantidades grandes", () => {
    expect(quantidade(950)).toBe("950");
    expect(nbsp(quantidade(15300))).toBe("15,3 mil");
  });

  it("converte texto em centavos", () => {
    expect(paraCentavos("R$ 12,90")).toBe(1290);
    expect(paraCentavos("1.299,00")).toBe(129900);
    expect(paraCentavos("")).toBeUndefined();
    expect(paraCentavos("abc")).toBeUndefined();
  });

  it("diz há quanto tempo", () => {
    const agora = new Date("2026-10-02T12:00:00Z");
    expect(haQuanto("2026-10-02T11:59:30Z", agora)).toBe("agora há pouco");
    expect(haQuanto("2026-10-02T11:15:00Z", agora)).toBe("há 45 min");
    expect(haQuanto("2026-10-02T06:00:00Z", agora)).toBe("há 6 h");
    expect(haQuanto("2026-09-30T12:00:00Z", agora)).toBe("há 2 dias");
  });

  it("formata tamanho de arquivo", () => {
    expect(tamanho(900)).toBe("900 bytes");
    expect(tamanho(1536)).toBe("1,5 KB");
    expect(tamanho(5 * 1024 ** 3)).toBe("5 GB");
  });

  it("formata a duração do vídeo", () => {
    expect(duracao(7)).toBe("0:07");
    expect(duracao(65)).toBe("1:05");
    expect(duracao(3725)).toBe("1:02:05");
  });
});
