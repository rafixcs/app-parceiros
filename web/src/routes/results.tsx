import { Link, useNavigate } from "@tanstack/react-router";
import { ArrowLeft, Info, RefreshCw, ShieldCheck, Users } from "lucide-react";
import type { ReactNode } from "react";
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import type { Schemas } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Notice, Card } from "@/components/ui/card";
import { count, money, timeAgo } from "@/lib/format";
import { cn } from "@/lib/utils";
import { useCurrentWorkspace, useShopeeConnection } from "./layout";
import { ProductImage } from "./radar";
import {
  fillDays,
  periods,
  useConsent,
  useGroupResults,
  useMyResults,
  useRequestSync,
  useSync,
  type Days,
} from "./results-api";
import { cohortResultsRoute, resultsRoute } from "./router";

const channels: Record<Schemas["ChannelResult"]["channel"], string> = {
  instagram: "Instagram",
  tiktok: "TikTok",
  whatsapp: "WhatsApp",
  other: "Link principal",
  "": "Links de fora do app",
};

const shortDay = new Intl.DateTimeFormat("pt-BR", { day: "2-digit", month: "2-digit", timeZone: "UTC" });

export function Results() {
  const { workspaceId } = resultsRoute.useParams();
  const { days = 30 } = resultsRoute.useSearch();
  const navigate = useNavigate({ from: resultsRoute.fullPath });
  const { workspace, manager } = useCurrentWorkspace(workspaceId);
  const r = useMyResults(workspaceId, days);

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-wrap items-end gap-3">
        <div className="mr-auto">
          <h1 className="text-2xl font-semibold">Resultados</h1>
          <p className="text-sm text-muted">
            {workspace?.kind === "mentorship"
              ? `Suas vendas pelos links gerados em ${workspace.name}.`
              : "Suas vendas pelos links do workspace pessoal e pelos links criados fora do app."}
          </p>
        </div>
        {manager && (
          <Link
            to="/w/$workspaceId/resultados/turma"
            params={{ workspaceId }}
            search={days === 30 ? {} : { days }}
            className="inline-flex h-10 items-center gap-2 rounded-lg border border-border bg-white px-4 text-sm font-medium hover:bg-zinc-50"
          >
            <Users className="size-4" /> Resultados da turma
          </Link>
        )}
        <PeriodPicker days={days} onChange={(d) => navigate({ search: d === 30 ? {} : { days: d } })} />
      </div>

      <SyncStatus />
      {r.data?.shares_results != null && <Consent workspaceId={workspaceId} sharesResults={r.data.shares_results} />}

      {r.error && <Notice className="border-red-200 bg-red-50 text-red-800">{r.error.message}</Notice>}
      {r.isPending && <p className="text-sm text-muted">Carregando…</p>}
      {r.data && (
        <>
          <Indicators totals={r.data.totals} />
          <DailyChart period={r.data.period} days={r.data.by_day} />
          <div className="grid gap-5 lg:grid-cols-3">
            <ProductTable workspaceId={workspaceId} products={r.data.by_product} className="lg:col-span-2" />
            <ChannelTable rows={r.data.by_channel} />
          </div>
          <ClicksNote />
        </>
      )}
    </div>
  );
}

export function CohortResults() {
  const { workspaceId } = cohortResultsRoute.useParams();
  const { days = 30 } = cohortResultsRoute.useSearch();
  const navigate = useNavigate({ from: cohortResultsRoute.fullPath });
  const { workspace } = useCurrentWorkspace(workspaceId);
  const r = useGroupResults(workspaceId, days);
  const t = r.data;

  return (
    <div className="flex flex-col gap-5">
      <Link
        to="/w/$workspaceId/resultados"
        params={{ workspaceId }}
        search={days === 30 ? {} : { days }}
        className="inline-flex items-center gap-1 text-sm text-muted hover:text-foreground"
      >
        <ArrowLeft className="size-4" /> Meus resultados
      </Link>
      <div className="flex flex-wrap items-end gap-3">
        <div className="mr-auto">
          <h1 className="text-2xl font-semibold">Resultados da turma</h1>
          <p className="text-sm text-muted">{workspace?.name}</p>
        </div>
        <PeriodPicker days={days} onChange={(d) => navigate({ search: d === 30 ? {} : { days: d } })} />
      </div>
      <Notice className="flex items-start gap-2 border-zinc-200 bg-white text-muted">
        <ShieldCheck className="mt-0.5 size-4 shrink-0 text-emerald-600" />
        <span>
          Os números somam só os membros que autorizaram mostrar os resultados, e nunca aparecem por afiliado. Cada
          afiliado pode mudar a autorização a qualquer momento em Resultados.
        </span>
      </Notice>

      {r.error && <Notice className="border-red-200 bg-red-50 text-red-800">{r.error.message}</Notice>}
      {r.isPending && <p className="text-sm text-muted">Carregando…</p>}
      {t && (
        <>
          <div className="grid grid-cols-3 gap-3">
            <Indicator label="Afiliados na turma" value={count(t.affiliates)} />
            <Indicator label="Autorizam ver resultados" value={count(t.sharing)} />
            <Indicator label="Venderam no período" value={count(t.active)} />
          </div>
          {t.sharing === 0 ? (
            <Card className="p-6 text-center text-sm text-muted">
              Ninguém da turma autorizou mostrar os resultados ainda. Quando alguém autorizar, a soma aparece aqui.
            </Card>
          ) : (
            <>
              <Indicators totals={t.totals} />
              <DailyChart period={t.period} days={t.by_day} />
              <ListTable workspaceId={workspaceId} lists={t.by_list} />
              <ProductTable workspaceId={workspaceId} products={t.by_product} />
            </>
          )}
          <ClicksNote />
        </>
      )}
    </div>
  );
}

function PeriodPicker({ days, onChange }: { days: Days; onChange: (d: Days) => void }) {
  return (
    <div className="flex rounded-lg border border-border bg-white p-0.5" role="group" aria-label="Período">
      {periods.map((d) => (
        <button
          key={d}
          type="button"
          aria-pressed={d === days}
          onClick={() => onChange(d)}
          className={cn("h-8 rounded-md px-3 text-sm", d === days ? "bg-zinc-900 text-white" : "hover:bg-zinc-100")}
        >
          {d} dias
        </button>
      ))}
    </div>
  );
}

function SyncStatus() {
  const connection = useShopeeConnection();
  const s = useSync();
  const sync = useRequestSync();
  if (connection.data && connection.data.status !== "connected") {
    return (
      <Notice className="border-amber-200 bg-amber-50 text-amber-900">
        Conecte a sua conta de afiliado da Shopee para trazer pedidos e comissões.{" "}
        <Link to="/conta/shopee" className="font-medium underline">
          Conectar a Shopee
        </Link>
      </Notice>
    );
  }
  const st = s.data;
  const running = st?.status === "syncing" || sync.isPending;
  return (
    <div className="flex flex-wrap items-center gap-3 text-sm text-muted">
      <span>
        {running
          ? "Buscando as conversões na Shopee…"
          : st?.finished_at
            ? `Atualizado ${timeAgo(st.finished_at)} com os pedidos dos últimos 89 dias. A Shopee é consultada uma vez por dia.`
            : "Os resultados ainda não foram buscados na Shopee."}
      </span>
      {st?.status === "error" && st.error && <span className="text-red-700">{st.error}</span>}
      <Button variant="secondary" size="sm" disabled={running} onClick={() => sync.mutate()}>
        <RefreshCw className={cn("size-4", running && "animate-spin")} /> Atualizar agora
      </Button>
      {sync.error && <span className="text-red-700">{sync.error.message}</span>}
    </div>
  );
}

function Consent({ workspaceId, sharesResults }: { workspaceId: string; sharesResults: boolean }) {
  const change = useConsent(workspaceId);
  const value = change.isPending ? change.variables : sharesResults;
  return (
    <Card className="flex flex-col gap-2 p-4 sm:flex-row sm:items-center">
      <label className="flex flex-1 cursor-pointer items-start gap-3">
        <input
          type="checkbox"
          className="mt-1 size-4 accent-brand"
          checked={value}
          disabled={change.isPending}
          onChange={(e) => change.mutate(e.target.checked)}
        />
        <span>
          <span className="block font-medium">Mostrar meus resultados ao mentor</span>
          <span className="block text-sm text-muted">
            O mentor vê só a soma da turma (pedidos e comissão por lista e por produto), nunca os seus números
            separados, nem a sua coleção ou as suas notas. Você pode desligar quando quiser.
          </span>
        </span>
      </label>
      {change.error && <p className="text-sm text-red-700">{change.error.message}</p>}
    </Card>
  );
}

function Indicators({ totals }: { totals: Schemas["ResultTotals"] }) {
  return (
    <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
      <Indicator
        label="Pedidos"
        value={count(totals.orders)}
        detail={totals.cancelled > 0 ? `${count(totals.cancelled)} cancelados` : undefined}
      />
      <Indicator label="Vendas" value={money(totals.sales_cents)} detail={`${count(totals.items)} itens`} />
      <Indicator label="Comissão estimada" value={money(totals.estimated_commission_cents)} highlight />
      <Indicator label="Comissão validada" value={money(totals.validated_commission_cents)} detail="pedidos concluídos" />
    </div>
  );
}

function Indicator({ label, value, detail, highlight }: { label: string; value: ReactNode; detail?: string; highlight?: boolean }) {
  return (
    <Card className="p-4">
      <p className="text-xs text-muted">{label}</p>
      <p className={cn("mt-1 text-xl font-semibold tabular-nums", highlight && "text-brand")}>{value}</p>
      {detail && <p className="text-xs text-muted">{detail}</p>}
    </Card>
  );
}

function DailyChart({ period, days }: { period: Schemas["Period"]; days: Schemas["DayResult"][] }) {
  const points = fillDays(period.from, period.to, days).map((d) => ({
    day: d.day,
    estimated: d.estimated_commission_cents / 100,
    validated: d.validated_commission_cents / 100,
    orders: d.orders,
  }));
  return (
    <Card className="p-4">
      <div className="mb-3 flex flex-wrap items-center gap-3">
        <h2 className="mr-auto font-medium">Comissão por dia</h2>
        <span className="flex items-center gap-1.5 text-xs text-muted">
          <span className="size-2.5 rounded-sm bg-brand" /> Validada
        </span>
        <span className="flex items-center gap-1.5 text-xs text-muted">
          <span className="size-2.5 rounded-sm bg-orange-200" /> Ainda não validada
        </span>
      </div>
      {days.length === 0 ? (
        <p className="py-10 text-center text-sm text-muted">Nenhuma venda no período.</p>
      ) : (
        <div className="h-56">
          <ResponsiveContainer>
            <BarChart data={points} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
              <CartesianGrid stroke="#e4e4e7" vertical={false} />
              <XAxis
                dataKey="day"
                tickFormatter={(d: string) => shortDay.format(new Date(d))}
                tick={{ fontSize: 12, fill: "#71717a" }}
                minTickGap={16}
              />
              <YAxis width={72} tickFormatter={(v: number) => money(v * 100)} tick={{ fontSize: 12, fill: "#71717a" }} />
              <Tooltip
                labelFormatter={(d) => shortDay.format(new Date(String(d)))}
                formatter={(v, name) => [money(Number(v) * 100), name === "validated" ? "Validada" : "Ainda não validada"]}
              />
              <Bar dataKey="validated" stackId="c" fill="#ee4d2d" isAnimationActive={false} />
              <Bar
                dataKey={(p: { estimated: number; validated: number }) => p.estimated - p.validated}
                name="pending"
                stackId="c"
                fill="#fed7aa"
                isAnimationActive={false}
              />
            </BarChart>
          </ResponsiveContainer>
        </div>
      )}
    </Card>
  );
}

function ProductTable({
  workspaceId,
  products,
  className,
}: {
  workspaceId: string;
  products: Schemas["ProductResult"][];
  className?: string;
}) {
  return (
    <Card className={cn("overflow-hidden", className)}>
      <h2 className="border-b border-border px-4 py-3 font-medium">Por produto</h2>
      {products.length === 0 ? (
        <p className="p-6 text-center text-sm text-muted">Nenhuma venda no período.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-left text-xs text-muted">
              <tr>
                <th className="px-4 py-2 font-normal">Produto</th>
                <th className="px-2 py-2 text-right font-normal">Pedidos</th>
                <th className="px-2 py-2 text-right font-normal">Estimada</th>
                <th className="px-4 py-2 text-right font-normal">Validada</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {products.map((p) => (
                <tr key={p.item_id}>
                  <td className="px-4 py-2">
                    <div className="flex min-w-48 items-center gap-3">
                      <ProductImage src={p.image_url} className="size-10 shrink-0 rounded-md" />
                      <span className="min-w-0">
                        {p.product_id ? (
                          <Link
                            to="/w/$workspaceId/radar/$productId"
                            params={{ workspaceId, productId: p.product_id }}
                            className="line-clamp-2 hover:underline"
                          >
                            {p.name}
                          </Link>
                        ) : (
                          <span className="line-clamp-2">{p.name}</span>
                        )}
                        <span className="block truncate text-xs text-muted">{p.shop_name}</span>
                      </span>
                    </div>
                  </td>
                  <td className="px-2 py-2 text-right tabular-nums">{count(p.orders)}</td>
                  <td className="px-2 py-2 text-right tabular-nums">{money(p.estimated_commission_cents)}</td>
                  <td className="px-4 py-2 text-right tabular-nums">{money(p.validated_commission_cents)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}

function ChannelTable({ rows }: { rows: Schemas["ChannelResult"][] }) {
  return (
    <Card className="self-start overflow-hidden">
      <h2 className="border-b border-border px-4 py-3 font-medium">Por canal</h2>
      {rows.length === 0 ? (
        <p className="p-6 text-center text-sm text-muted">Nenhuma venda no período.</p>
      ) : (
        <ul className="divide-y divide-border text-sm">
          {rows.map((c) => (
            <li key={c.channel} className="flex items-center gap-2 px-4 py-2">
              <span className="flex-1">{channels[c.channel] ?? c.channel}</span>
              <span className="text-muted tabular-nums">{count(c.orders)} ped.</span>
              <span className="w-24 text-right font-medium tabular-nums">{money(c.estimated_commission_cents)}</span>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

function ListTable({ workspaceId, lists }: { workspaceId: string; lists: Schemas["ListResult"][] }) {
  return (
    <Card className="overflow-hidden">
      <div className="border-b border-border px-4 py-3">
        <h2 className="font-medium">Por lista</h2>
        <p className="text-xs text-muted">
          Vendas dos produtos de cada lista feitas por quem a importou, depois da importação.
        </p>
      </div>
      {lists.length === 0 ? (
        <p className="p-6 text-center text-sm text-muted">Nenhuma lista publicada ainda.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-left text-xs text-muted">
              <tr>
                <th className="px-4 py-2 font-normal">Lista</th>
                <th className="px-2 py-2 text-right font-normal">Importaram</th>
                <th className="px-2 py-2 text-right font-normal">Pedidos</th>
                <th className="px-2 py-2 text-right font-normal">Estimada</th>
                <th className="px-4 py-2 text-right font-normal">Validada</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {lists.map((l) => (
                <tr key={l.id}>
                  <td className="px-4 py-2">
                    <Link
                      to="/w/$workspaceId/listas/$listId"
                      params={{ workspaceId, listId: l.id }}
                      className="hover:underline"
                    >
                      {l.title}
                    </Link>
                  </td>
                  <td className="px-2 py-2 text-right tabular-nums">{count(l.importers)}</td>
                  <td className="px-2 py-2 text-right tabular-nums">{count(l.orders)}</td>
                  <td className="px-2 py-2 text-right tabular-nums">{money(l.estimated_commission_cents)}</td>
                  <td className="px-4 py-2 text-right tabular-nums">{money(l.validated_commission_cents)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}

function ClicksNote() {
  return (
    <p className="flex items-start gap-2 text-xs text-muted">
      <Info className="mt-0.5 size-3.5 shrink-0" />
      <span>
        Os números vêm do relatório de conversões da API oficial da Shopee. Ela não informa cliques; para vê-los, use o
        painel de afiliados da Shopee. A comissão validada soma os pedidos concluídos.
      </span>
    </p>
  );
}
