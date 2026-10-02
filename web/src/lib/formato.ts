const real = new Intl.NumberFormat("pt-BR", { style: "currency", currency: "BRL" });
const inteiro = new Intl.NumberFormat("pt-BR");
const compacto = new Intl.NumberFormat("pt-BR", { notation: "compact", maximumFractionDigits: 1 });

/** 12990 → "R$ 129,90" */
export function reais(centavos: number): string {
  return real.format(centavos / 100);
}

/** Faixa de preço: um valor só quando mínimo e máximo coincidem. */
export function faixaPreco(min: number, max: number): string {
  return min === max ? reais(min) : `${reais(min)} – ${reais(max)}`;
}

/** 1250 → "12,5%" */
export function porcentagem(bp: number): string {
  return `${inteiro.format(bp / 100)}%`;
}

/** 15300 → "15,3 mil" */
export function quantidade(n: number): string {
  return n < 10000 ? inteiro.format(n) : compacto.format(n);
}

/** "R$ 12,90" ou "12,90" → 1290; vazio ou inválido → undefined */
export function paraCentavos(texto: string): number | undefined {
  const limpo = texto.replace(/[R$\s.]/g, "").replace(",", ".");
  if (limpo === "") return undefined;
  const v = Number(limpo);
  return Number.isFinite(v) && v >= 0 ? Math.round(v * 100) : undefined;
}

/** Tempo desde `iso`, em português: "agora há pouco", "há 5 min", "há 3 h", "há 2 dias". */
export function haQuanto(iso: string, agora: Date = new Date()): string {
  const s = Math.max(0, (agora.getTime() - new Date(iso).getTime()) / 1000);
  if (s < 60) return "agora há pouco";
  const min = Math.floor(s / 60);
  if (min < 60) return `há ${min} min`;
  const h = Math.floor(min / 60);
  if (h < 24) return `há ${h} h`;
  const d = Math.floor(h / 24);
  return d === 1 ? "há 1 dia" : `há ${d} dias`;
}
