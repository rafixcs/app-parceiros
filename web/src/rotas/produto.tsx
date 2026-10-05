import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowLeft, ExternalLink, Star } from "lucide-react";
import { useState } from "react";
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { api, exigir, type Schemas } from "@/api/cliente";
import { Button } from "@/components/ui/button";
import { Aviso, Card } from "@/components/ui/card";
import { faixaPreco, haQuanto, porcentagem, quantidade, reais } from "@/lib/formato";
import { cn } from "@/lib/utils";
import { Imagem, Tendencia } from "./radar";
import { rotaProduto } from "./router";
import { BotaoSalvar } from "./salvar";
import { VideosDoProduto } from "./videos-produto";

const series = {
  preco: { rotulo: "Preço", valor: (s: Schemas["Snapshot"]) => s.preco_min_centavos / 100, formato: (v: number) => reais(v * 100) },
  comissao: { rotulo: "Comissão", valor: (s: Schemas["Snapshot"]) => s.comissao_bp / 100, formato: (v: number) => porcentagem(v * 100) },
  vendas: { rotulo: "Vendas", valor: (s: Schemas["Snapshot"]) => s.vendas, formato: quantidade },
} as const;
type Serie = keyof typeof series;

const dataCurta = new Intl.DateTimeFormat("pt-BR", { day: "2-digit", month: "2-digit" });
const dataHora = new Intl.DateTimeFormat("pt-BR", { dateStyle: "short", timeStyle: "short" });

export function Produto() {
  const { workspaceId, produtoId } = rotaProduto.useParams();
  const [serie, setSerie] = useState<Serie>("vendas");
  const [dias, setDias] = useState(30);
  const { data, error, isPending } = useQuery({
    queryKey: ["radar-produto", workspaceId, produtoId, dias],
    queryFn: () =>
      exigir(
        api.GET("/v1/workspaces/{workspaceId}/radar/produtos/{produtoId}", {
          params: { path: { workspaceId, produtoId }, query: { dias } },
        }),
      ),
  });

  const voltar = (
    <Link
      to="/w/$workspaceId/radar"
      params={{ workspaceId }}
      search={{}}
      className="inline-flex items-center gap-1 text-sm text-suave hover:text-texto"
    >
      <ArrowLeft className="size-4" /> Radar
    </Link>
  );
  if (error) return <div className="flex flex-col gap-4">{voltar}<Aviso className="border-red-200 bg-red-50 text-red-800">{error.message}</Aviso></div>;
  if (isPending) return <p className="text-sm text-suave">Carregando…</p>;

  const p = data.produto;
  const s = series[serie];
  const pontos = data.historico.map((h) => ({ t: new Date(h.coletado_em).getTime(), v: s.valor(h) }));

  return (
    <div className="flex flex-col gap-4">
      {voltar}
      <Card className="flex flex-col gap-4 p-4 sm:flex-row">
        <Imagem src={p.imagem_url} className="aspect-square w-full rounded-lg sm:w-48" />
        <div className="flex flex-1 flex-col gap-2">
          <h1 className="text-xl font-semibold leading-snug">{p.nome}</h1>
          <p className="text-sm text-suave">{p.loja_nome}</p>
          <dl className="mt-2 grid grid-cols-2 gap-3 sm:grid-cols-4">
            <Dado rotulo="Preço" valor={faixaPreco(p.preco_min_centavos, p.preco_max_centavos)} />
            <Dado rotulo="Comissão" valor={porcentagem(p.comissao_bp)} />
            <Dado rotulo="Você ganha por venda" valor={reais(p.ganho_por_venda_centavos)} destaque />
            <Dado
              rotulo="Vendas"
              valor={
                <>
                  {quantidade(p.vendas)}
                  {p.vendas_7d != null && <span className="text-xs text-emerald-700"> +{quantidade(p.vendas_7d)} em 7 dias</span>}
                </>
              }
            />
          </dl>
          <div className="mt-auto flex flex-wrap items-center gap-3 text-sm text-suave">
            <Tendencia produto={p} />
            {p.nota != null && (
              <span className="flex items-center gap-1">
                <Star className="size-4 fill-amber-400 text-amber-400" /> {p.nota.toLocaleString("pt-BR")}
              </span>
            )}
            <span>Atualizado {haQuanto(p.atualizado_em)}</span>
            <a href={p.url} target="_blank" rel="noreferrer" className="ml-auto inline-flex items-center gap-1 text-marca hover:underline">
              Ver na Shopee <ExternalLink className="size-3.5" />
            </a>
            <BotaoSalvar workspaceId={workspaceId} produtoId={p.produto_id} />
          </div>
        </div>
      </Card>

      <VideosDoProduto workspaceId={workspaceId} produtoId={p.produto_id} />

      <Card className="p-4">
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <h2 className="mr-auto font-medium">Histórico</h2>
          {(Object.keys(series) as Serie[]).map((k) => (
            <Button key={k} tamanho="sm" variante={k === serie ? "primario" : "secundario"} onClick={() => setSerie(k)}>
              {series[k].rotulo}
            </Button>
          ))}
          <select
            aria-label="Período"
            className="h-8 rounded-lg border border-borda bg-white px-2 text-sm"
            value={dias}
            onChange={(e) => setDias(Number(e.target.value))}
          >
            <option value={7}>7 dias</option>
            <option value={30}>30 dias</option>
            <option value={90}>90 dias</option>
          </select>
        </div>
        {pontos.length < 2 ? (
          <p className="py-10 text-center text-sm text-suave">
            O gráfico aparece quando houver pelo menos duas coletas deste produto.
          </p>
        ) : (
          <div className="h-64">
            <ResponsiveContainer>
              <LineChart data={pontos} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
                <CartesianGrid stroke="#e4e4e7" vertical={false} />
                <XAxis
                  dataKey="t"
                  type="number"
                  scale="time"
                  domain={["dataMin", "dataMax"]}
                  tickFormatter={(t: number) => dataCurta.format(t)}
                  tick={{ fontSize: 12, fill: "#71717a" }}
                />
                <YAxis width={72} tickFormatter={s.formato} tick={{ fontSize: 12, fill: "#71717a" }} domain={["auto", "auto"]} />
                <Tooltip
                  labelFormatter={(t) => dataHora.format(Number(t))}
                  formatter={(v) => [s.formato(Number(v)), s.rotulo]}
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

function Dado({ rotulo, valor, destaque }: { rotulo: string; valor: React.ReactNode; destaque?: boolean }) {
  return (
    <div>
      <dt className="text-xs text-suave">{rotulo}</dt>
      <dd className={cn("font-medium", destaque && "text-emerald-700")}>{valor}</dd>
    </div>
  );
}
