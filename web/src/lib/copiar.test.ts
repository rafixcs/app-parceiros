import { describe, expect, it } from "vitest";
import { linkDoCanal, textoParaCopiar } from "./copiar";

const base = {
  titulo: "Fone que vende muito",
  descricao: "Bateria de 30 h",
  produto: { nome: "Fone Bluetooth XYZ" },
  link_afiliado: "https://s.shopee.com.br/outro",
  link_origem: "auto" as const,
  links: [
    { canal: "instagram" as const, url: "https://s.shopee.com.br/insta" },
    { canal: "outro" as const, url: "https://s.shopee.com.br/outro" },
  ],
};

describe("copiar rápido", () => {
  it("usa o link do canal", () => {
    expect(textoParaCopiar(base, "instagram")).toBe(
      "Fone que vende muito\n\nBateria de 30 h\n\nhttps://s.shopee.com.br/insta",
    );
  });

  it("cai no link principal quando o canal não tem link", () => {
    expect(linkDoCanal(base, "tiktok")).toBe("https://s.shopee.com.br/outro");
  });

  it("o link manual vale para todos os canais", () => {
    const manual = { ...base, link_origem: "manual" as const, link_afiliado: "https://meu.link/x" };
    expect(linkDoCanal(manual, "instagram")).toBe("https://meu.link/x");
  });

  it("sem título usa o nome do produto e pula a descrição vazia", () => {
    expect(textoParaCopiar({ ...base, titulo: " ", descricao: "" }, "outro")).toBe(
      "Fone Bluetooth XYZ\n\nhttps://s.shopee.com.br/outro",
    );
  });

  it("sem link copia só o texto", () => {
    expect(textoParaCopiar({ ...base, link_afiliado: null, links: [] }, "whatsapp")).toBe(
      "Fone que vende muito\n\nBateria de 30 h",
    );
  });
});
