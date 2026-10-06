import { describe, expect, it } from "vitest";
import { channelLink, textToCopy } from "./clipboard";

const base = {
  title: "Fone que vende muito",
  description: "Bateria de 30 h",
  product: { name: "Fone Bluetooth XYZ" },
  affiliate_link: "https://s.shopee.com.br/outro",
  link_origin: "auto" as const,
  links: [
    { channel: "instagram" as const, url: "https://s.shopee.com.br/insta" },
    { channel: "other" as const, url: "https://s.shopee.com.br/outro" },
  ],
};

describe("quick copy", () => {
  it("uses the channel's link", () => {
    expect(textToCopy(base, "instagram")).toBe(
      "Fone que vende muito\n\nBateria de 30 h\n\nhttps://s.shopee.com.br/insta",
    );
  });

  it("falls back to the main link when the channel has none", () => {
    expect(channelLink(base, "tiktok")).toBe("https://s.shopee.com.br/outro");
  });

  it("the manual link counts for every channel", () => {
    const manual = { ...base, link_origin: "manual" as const, affiliate_link: "https://meu.link/x" };
    expect(channelLink(manual, "instagram")).toBe("https://meu.link/x");
  });

  it("without a title uses the product name and skips an empty description", () => {
    expect(textToCopy({ ...base, title: " ", description: "" }, "other")).toBe(
      "Fone Bluetooth XYZ\n\nhttps://s.shopee.com.br/outro",
    );
  });

  it("without a link copies only the text", () => {
    expect(textToCopy({ ...base, affiliate_link: null, links: [] }, "whatsapp")).toBe(
      "Fone que vende muito\n\nBateria de 30 h",
    );
  });
});
