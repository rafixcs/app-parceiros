import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowLeft, ExternalLink, Star } from "lucide-react";
import { useState } from "react";
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { api, unwrap, type Schemas } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Card, Notice } from "@/components/ui/card";
import { count, money, percent, priceRange, timeAgo } from "@/lib/format";
import { cn } from "@/lib/utils";
import { ProductVideos } from "./product-videos";
import { ProductImage, TrendBadge } from "./radar";
import { productRoute } from "./router";
import { SaveButton } from "./save";

const series = {
  price: { label: "Preço", value: (s: Schemas["Snapshot"]) => s.min_price_cents / 100, format: (v: number) => money(v * 100) },
  commission: { label: "Comissão", value: (s: Schemas["Snapshot"]) => s.commission_bp / 100, format: (v: number) => percent(v * 100) },
  sales: { label: "Vendas", value: (s: Schemas["Snapshot"]) => s.sales, format: count },
} as const;
type Series = keyof typeof series;

const shortDate = new Intl.DateTimeFormat("pt-BR", { day: "2-digit", month: "2-digit" });
const dateTime = new Intl.DateTimeFormat("pt-BR", { dateStyle: "short", timeStyle: "short" });

export function ProductPage() {
  const { workspaceId, productId } = productRoute.useParams();
  const [serie, setSerie] = useState<Series>("sales");
  const [days, setDays] = useState(30);
  const { data, error, isPending } = useQuery({
    queryKey: ["radar-product", workspaceId, productId, days],
    queryFn: () =>
      unwrap(
        api.GET("/v1/workspaces/{workspaceId}/radar/products/{productId}", {
          params: { path: { workspaceId, productId }, query: { days } },
        }),
      ),
  });

  const back = (
    <Link
      to="/w/$workspaceId/radar"
      params={{ workspaceId }}
      search={{}}
      className="inline-flex items-center gap-1 text-sm text-muted hover:text-foreground"
    >
      <ArrowLeft className="size-4" /> Radar
    </Link>
  );
  if (error) return <div className="flex flex-col gap-4">{back}<Notice className="border-red-200 bg-red-50 text-red-800">{error.message}</Notice></div>;
  if (isPending) return <p className="text-sm text-muted">Carregando…</p>;

  const p = data.product;
  const s = series[serie];
  const points = data.history.map((h) => ({ t: new Date(h.collected_at).getTime(), v: s.value(h) }));

  return (
    <div className="flex flex-col gap-4">
      {back}
      <Card className="flex flex-col gap-4 p-4 sm:flex-row">
        <ProductImage src={p.image_url} className="aspect-square w-full rounded-lg sm:w-48" />
        <div className="flex flex-1 flex-col gap-2">
          <h1 className="text-xl font-semibold leading-snug">{p.name}</h1>
          <p className="text-sm text-muted">{p.shop_name}</p>
          <dl className="mt-2 grid grid-cols-2 gap-3 sm:grid-cols-4">
            <Stat label="Preço" value={priceRange(p.min_price_cents, p.max_price_cents)} />
            <Stat label="Comissão" value={percent(p.commission_bp)} />
            <Stat label="Você ganha por venda" value={money(p.earnings_per_sale_cents)} highlight />
            <Stat
              label="Vendas"
              value={
                <>
                  {count(p.sales)}
                  {p.sales_growth_7d != null && <span className="text-xs text-emerald-700"> +{count(p.sales_growth_7d)} em 7 dias</span>}
                </>
              }
            />
          </dl>
          <div className="mt-auto flex flex-wrap items-center gap-3 text-sm text-muted">
            <TrendBadge product={p} />
            {p.rating != null && (
              <span className="flex items-center gap-1">
                <Star className="size-4 fill-amber-400 text-amber-400" /> {p.rating.toLocaleString("pt-BR")}
              </span>
            )}
            <span>Atualizado {timeAgo(p.updated_at)}</span>
            <a href={p.url} target="_blank" rel="noreferrer" className="ml-auto inline-flex items-center gap-1 text-brand hover:underline">
              Ver na Shopee <ExternalLink className="size-3.5" />
            </a>
            <SaveButton workspaceId={workspaceId} productId={p.product_id} />
          </div>
        </div>
      </Card>

      <ProductVideos workspaceId={workspaceId} productId={p.product_id} />

      <Card className="p-4">
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <h2 className="mr-auto font-medium">Histórico</h2>
          {(Object.keys(series) as Series[]).map((k) => (
            <Button key={k} size="sm" variant={k === serie ? "primary" : "secondary"} onClick={() => setSerie(k)}>
              {series[k].label}
            </Button>
          ))}
          <select
            aria-label="Período"
            className="h-8 rounded-lg border border-border bg-white px-2 text-sm"
            value={days}
            onChange={(e) => setDays(Number(e.target.value))}
          >
            <option value={7}>7 dias</option>
            <option value={30}>30 dias</option>
            <option value={90}>90 dias</option>
          </select>
        </div>
        {points.length < 2 ? (
          <p className="py-10 text-center text-sm text-muted">
            O gráfico aparece quando houver pelo menos duas coletas deste produto.
          </p>
        ) : (
          <div className="h-64">
            <ResponsiveContainer>
              <LineChart data={points} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
                <CartesianGrid stroke="#e4e4e7" vertical={false} />
                <XAxis
                  dataKey="t"
                  type="number"
                  scale="time"
                  domain={["dataMin", "dataMax"]}
                  tickFormatter={(t: number) => shortDate.format(t)}
                  tick={{ fontSize: 12, fill: "#71717a" }}
                />
                <YAxis width={72} tickFormatter={s.format} tick={{ fontSize: 12, fill: "#71717a" }} domain={["auto", "auto"]} />
                <Tooltip
                  labelFormatter={(t) => dateTime.format(Number(t))}
                  formatter={(v) => [s.format(Number(v)), s.label]}
                />
                <Line type="monotone" dataKey="v" stroke="#ee4d2d" strokeWidth={2} dot={false} isAnimationActive={false} />
              </LineChart>
            </ResponsiveContainer>
          </div>
        )}
      </Card>
    </div>
  );
}

function Stat({ label, value, highlight }: { label: string; value: React.ReactNode; highlight?: boolean }) {
  return (
    <div>
      <dt className="text-xs text-muted">{label}</dt>
      <dd className={cn("font-medium", highlight && "text-emerald-700")}>{value}</dd>
    </div>
  );
}
