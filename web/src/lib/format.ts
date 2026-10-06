const brl = new Intl.NumberFormat("pt-BR", { style: "currency", currency: "BRL" });
const integer = new Intl.NumberFormat("pt-BR");
const compact = new Intl.NumberFormat("pt-BR", { notation: "compact", maximumFractionDigits: 1 });

/** 12990 → "R$ 129,90" */
export function money(cents: number): string {
  return brl.format(cents / 100);
}

/** Price range: a single value when min and max are the same. */
export function priceRange(min: number, max: number): string {
  return min === max ? money(min) : `${money(min)} – ${money(max)}`;
}

/** 1250 → "12,5%" */
export function percent(bp: number): string {
  return `${integer.format(bp / 100)}%`;
}

/** 15300 → "15,3 mil" */
export function count(n: number): string {
  return n < 10000 ? integer.format(n) : compact.format(n);
}

/** "R$ 12,90" or "12,90" → 1290; empty or invalid → undefined */
export function toCents(text: string): number | undefined {
  const clean = text.replace(/[R$\s.]/g, "").replace(",", ".");
  if (clean === "") return undefined;
  const v = Number(clean);
  return Number.isFinite(v) && v >= 0 ? Math.round(v * 100) : undefined;
}

/** Time since `iso`, in Portuguese: "agora há pouco", "há 5 min", "há 3 h", "há 2 dias". */
export function timeAgo(iso: string, now: Date = new Date()): string {
  const s = Math.max(0, (now.getTime() - new Date(iso).getTime()) / 1000);
  if (s < 60) return "agora há pouco";
  const min = Math.floor(s / 60);
  if (min < 60) return `há ${min} min`;
  const h = Math.floor(min / 60);
  if (h < 24) return `há ${h} h`;
  const d = Math.floor(h / 24);
  return d === 1 ? "há 1 dia" : `há ${d} dias`;
}

const decimal = new Intl.NumberFormat("pt-BR", { maximumFractionDigits: 1 });

/** 1536 → "1,5 KB"; 5368709120 → "5 GB" */
export function fileSize(bytes: number): string {
  const units = ["bytes", "KB", "MB", "GB", "TB"];
  let v = bytes;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${i === 0 ? integer.format(v) : decimal.format(v)} ${units[i]}`;
}

/** 65 → "1:05"; 3725 → "1:02:05" */
export function duration(seconds: number): string {
  const s = Math.max(0, Math.round(seconds));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const ss = String(s % 60).padStart(2, "0");
  return h > 0 ? `${h}:${String(m).padStart(2, "0")}:${ss}` : `${m}:${ss}`;
}
