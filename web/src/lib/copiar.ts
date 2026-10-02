export type Canal = "instagram" | "tiktok" | "whatsapp" | "outro";

export const canais: Record<Canal, string> = {
  instagram: "Instagram",
  tiktok: "TikTok",
  whatsapp: "WhatsApp",
  outro: "Outro",
};

type ItemParaCopiar = {
  titulo: string;
  descricao: string;
  produto: { nome: string };
  link_afiliado: string | null;
  link_origem: "auto" | "manual";
  links: { canal: Canal; url: string }[];
};

/** Link do item para o canal: o manual vale para todos; senão, o do canal ou o principal. */
export function linkDoCanal(item: ItemParaCopiar, canal: Canal): string | null {
  if (item.link_origem === "manual") return item.link_afiliado;
  return item.links.find((l) => l.canal === canal)?.url ?? item.link_afiliado;
}

/** Título, descrição e link, separados por linha em branco, prontos para colar numa rede social. */
export function textoParaCopiar(item: ItemParaCopiar, canal: Canal): string {
  const titulo = item.titulo.trim() || item.produto.nome;
  return [titulo, item.descricao.trim(), linkDoCanal(item, canal) ?? ""].filter(Boolean).join("\n\n");
}

/** Copia para a área de transferência; devolve false se o navegador não deixar. */
export async function copiar(texto: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(texto);
    return true;
  } catch {
    return false;
  }
}
