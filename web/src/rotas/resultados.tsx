import { Link, useNavigate } from "@tanstack/react-router";
import { ArrowLeft, Info, RefreshCw, ShieldCheck, Users } from "lucide-react";
import type { ReactNode } from "react";
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import type { Schemas } from "@/api/cliente";
import { Button } from "@/components/ui/button";
import { Aviso, Card } from "@/components/ui/card";
import { haQuanto, quantidade, reais } from "@/lib/formato";
import { cn } from "@/lib/utils";
import { useConexaoShopee } from "./layout";
import { useWorkspaceAtual } from "./listas-api";
import { Imagem } from "./radar";
import {
  periodos,
  preencherDias,
  useConsentimento,
  useMeusResultados,
  useResultadosTurma,
  useSincronizacao,
  useSincronizar,
  type Dias,
} from "./resultados-api";
import { rotaResultados, rotaResultadosTurma } from "./router";

const canais: Record<string, string> = {
  instagram: "Instagram",
  tiktok: "TikTok",
  whatsapp: "WhatsApp",
  outro: "Link principal",
  "": "Links de fora do app",
};

const diaCurto = new Intl.DateTimeFormat("pt-BR", { day: "2-digit", month: "2-digit", timeZone: "UTC" });

export function Resultados() {
  const { workspaceId } = rotaResultados.useParams();
  const { dias = 30 } = rotaResultados.useSearch();
  const navigate = useNavigate({ from: rotaResultados.fullPath });
  const { workspace, gestor } = useWorkspaceAtual(workspaceId);
  const r = useMeusResultados(workspaceId, dias);

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-wrap items-end gap-3">
        <div className="mr-auto">
          <h1 className="text-2xl font-semibold">Resultados</h1>
          <p className="text-sm text-suave">
            {workspace?.tipo === "mentoria"
              ? `Suas vendas pelos links gerados em ${workspace.nome}.`
              : "Suas vendas pelos links do workspace pessoal e pelos links criados fora do app."}
          </p>
        </div>
        {gestor && (
          <Link
            to="/w/$workspaceId/resultados/turma"
            params={{ workspaceId }}
            search={dias === 30 ? {} : { dias }}
            className="inline-flex h-10 items-center gap-2 rounded-lg border border-borda bg-white px-4 text-sm font-medium hover:bg-zinc-50"
          >
            <Users className="size-4" /> Resultados da turma
          </Link>
        )}
        <SeletorPeriodo dias={dias} onChange={(d) => navigate({ search: d === 30 ? {} : { dias: d } })} />
      </div>

      <Sincronizacao />
      {r.data?.consente != null && <Consentimento workspaceId={workspaceId} consente={r.data.consente} />}

      {r.error && <Aviso className="border-red-200 bg-red-50 text-red-800">{r.error.message}</Aviso>}
      {r.isPending && <p className="text-sm text-suave">Carregando…</p>}
      {r.data && (
        <>
          <Indicadores totais={r.data.totais} />
          <GraficoDias periodo={r.data.periodo} dias={r.data.por_dia} />
          <div className="grid gap-5 lg:grid-cols-3">
            <TabelaProdutos workspaceId={workspaceId} produtos={r.data.por_produto} className="lg:col-span-2" />
            <TabelaCanais canais={r.data.por_canal} />
          </div>
          <NotaCliques />
        </>
      )}
    </div>
  );
}

export function ResultadosTurma() {
  const { workspaceId } = rotaResultadosTurma.useParams();
  const { dias = 30 } = rotaResultadosTurma.useSearch();
  const navigate = useNavigate({ from: rotaResultadosTurma.fullPath });
  const { workspace } = useWorkspaceAtual(workspaceId);
  const r = useResultadosTurma(workspaceId, dias);
  const t = r.data;

  return (
    <div className="flex flex-col gap-5">
      <Link
        to="/w/$workspaceId/resultados"
        params={{ workspaceId }}
        search={dias === 30 ? {} : { dias }}
        className="inline-flex items-center gap-1 text-sm text-suave hover:text-texto"
      >
        <ArrowLeft className="size-4" /> Meus resultados
      </Link>
      <div className="flex flex-wrap items-end gap-3">
        <div className="mr-auto">
          <h1 className="text-2xl font-semibold">Resultados da turma</h1>
          <p className="text-sm text-suave">{workspace?.nome}</p>
        </div>
        <SeletorPeriodo dias={dias} onChange={(d) => navigate({ search: d === 30 ? {} : { dias: d } })} />
      </div>
      <Aviso className="flex items-start gap-2 border-zinc-200 bg-white text-suave">
        <ShieldCheck className="mt-0.5 size-4 shrink-0 text-emerald-600" />
        <span>
          Os números somam só os membros que autorizaram mostrar os resultados, e nunca aparecem por afiliado. Cada
          afiliado pode mudar a autorização a qualquer momento em Resultados.
        </span>
      </Aviso>

      {r.error && <Aviso className="border-red-200 bg-red-50 text-red-800">{r.error.message}</Aviso>}
      {r.isPending && <p className="text-sm text-suave">Carregando…</p>}
      {t && (
        <>
          <div className="grid grid-cols-3 gap-3">
            <Indicador rotulo="Afiliados na turma" valor={quantidade(t.afiliados)} />
            <Indicador rotulo="Autorizam ver resultados" valor={quantidade(t.consentem)} />
            <Indicador rotulo="Venderam no período" valor={quantidade(t.ativos)} />
          </div>
          {t.consentem === 0 ? (
            <Card className="p-6 text-center text-sm text-suave">
              Ninguém da turma autorizou mostrar os resultados ainda. Quando alguém autorizar, a soma aparece aqui.
            </Card>
          ) : (
            <>
              <Indicadores totais={t.totais} />
              <GraficoDias periodo={t.periodo} dias={t.por_dia} />
              <TabelaListas workspaceId={workspaceId} listas={t.por_lista} />
              <TabelaProdutos workspaceId={workspaceId} produtos={t.por_produto} />
            </>
          )}
          <NotaCliques />
        </>
      )}
    </div>
  );
}

function SeletorPeriodo({ dias, onChange }: { dias: Dias; onChange: (d: Dias) => void }) {
  return (
    <div className="flex rounded-lg border border-borda bg-white p-0.5" role="group" aria-label="Período">
      {periodos.map((d) => (
        <button
          key={d}
          type="button"
          aria-pressed={d === dias}
          onClick={() => onChange(d)}
          className={cn("h-8 rounded-md px-3 text-sm", d === dias ? "bg-zinc-900 text-white" : "hover:bg-zinc-100")}
        >
          {d} dias
        </button>
      ))}
    </div>
  );
}

function Sincronizacao() {
  const conexao = useConexaoShopee();
  const s = useSincronizacao();
  const sincronizar = useSincronizar();
  if (conexao.data && conexao.data.status !== "conectado") {
    return (
      <Aviso className="border-amber-200 bg-amber-50 text-amber-900">
        Conecte a sua conta de afiliado da Shopee para trazer pedidos e comissões.{" "}
        <Link to="/conta/shopee" className="font-medium underline">
          Conectar a Shopee
        </Link>
      </Aviso>
    );
  }
  const st = s.data;
  const rodando = st?.status === "sincronizando" || sincronizar.isPending;
  return (
    <div className="flex flex-wrap items-center gap-3 text-sm text-suave">
      <span>
        {rodando
          ? "Buscando as conversões na Shopee…"
          : st?.atualizado_em
            ? `Atualizado ${haQuanto(st.atualizado_em)} com os pedidos dos últimos 89 dias. A Shopee é consultada uma vez por dia.`
            : "Os resultados ainda não foram buscados na Shopee."}
      </span>
      {st?.status === "erro" && st.erro && <span className="text-red-700">{st.erro}</span>}
      <Button variante="secundario" tamanho="sm" disabled={rodando} onClick={() => sincronizar.mutate()}>
        <RefreshCw className={cn("size-4", rodando && "animate-spin")} /> Atualizar agora
      </Button>
      {sincronizar.error && <span className="text-red-700">{sincronizar.error.message}</span>}
    </div>
  );
}

function Consentimento({ workspaceId, consente }: { workspaceId: string; consente: boolean }) {
  const mudar = useConsentimento(workspaceId);
  const valor = mudar.isPending ? mudar.variables : consente;
  return (
    <Card className="flex flex-col gap-2 p-4 sm:flex-row sm:items-center">
      <label className="flex flex-1 cursor-pointer items-start gap-3">
        <input
          type="checkbox"
          className="mt-1 size-4 accent-marca"
          checked={valor}
          disabled={mudar.isPending}
          onChange={(e) => mudar.mutate(e.target.checked)}
        />
        <span>
          <span className="block font-medium">Mostrar meus resultados ao mentor</span>
          <span className="block text-sm text-suave">
            O mentor vê só a soma da turma (pedidos e comissão por lista e por produto), nunca os seus números
            separados, nem a sua coleção ou as suas notas. Você pode desligar quando quiser.
          </span>
        </span>
      </label>
      {mudar.error && <p className="text-sm text-red-700">{mudar.error.message}</p>}
    </Card>
  );
}

function Indicadores({ totais }: { totais: Schemas["TotaisResultados"] }) {
  return (
    <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
      <Indicador rotulo="Pedidos" valor={quantidade(totais.pedidos)} detalhe={totais.cancelados > 0 ? `${quantidade(totais.cancelados)} cancelados` : undefined} />
      <Indicador rotulo="Vendas" valor={reais(totais.vendas_centavos)} detalhe={`${quantidade(totais.itens)} itens`} />
      <Indicador rotulo="Comissão estimada" valor={reais(totais.comissao_estimada_centavos)} destaque />
      <Indicador rotulo="Comissão validada" valor={reais(totais.comissao_validada_centavos)} detalhe="pedidos concluídos" />
    </div>
  );
}

function Indicador({ rotulo, valor, detalhe, destaque }: { rotulo: string; valor: ReactNode; detalhe?: string; destaque?: boolean }) {
  return (
    <Card className="p-4">
      <p className="text-xs text-suave">{rotulo}</p>
      <p className={cn("mt-1 text-xl font-semibold tabular-nums", destaque && "text-marca")}>{valor}</p>
      {detalhe && <p className="text-xs text-suave">{detalhe}</p>}
    </Card>
  );
}

function GraficoDias({ periodo, dias }: { periodo: Schemas["Periodo"]; dias: Schemas["ResultadoDia"][] }) {
  const pontos = preencherDias(periodo.de, periodo.ate, dias).map((d) => ({
    dia: d.dia,
    estimada: d.comissao_estimada_centavos / 100,
    validada: d.comissao_validada_centavos / 100,
    pedidos: d.pedidos,
  }));
  return (
    <Card className="p-4">
      <div className="mb-3 flex flex-wrap items-center gap-3">
        <h2 className="mr-auto font-medium">Comissão por dia</h2>
        <span className="flex items-center gap-1.5 text-xs text-suave">
          <span className="size-2.5 rounded-sm bg-marca" /> Validada
        </span>
        <span className="flex items-center gap-1.5 text-xs text-suave">
          <span className="size-2.5 rounded-sm bg-orange-200" /> Ainda não validada
        </span>
      </div>
      {dias.length === 0 ? (
        <p className="py-10 text-center text-sm text-suave">Nenhuma venda no período.</p>
      ) : (
        <div className="h-56">
          <ResponsiveContainer>
            <BarChart data={pontos} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
              <CartesianGrid stroke="#e4e4e7" vertical={false} />
              <XAxis
                dataKey="dia"
                tickFormatter={(d: string) => diaCurto.format(new Date(d))}
                tick={{ fontSize: 12, fill: "#71717a" }}
                minTickGap={16}
              />
              <YAxis width={72} tickFormatter={(v: number) => reais(v * 100)} tick={{ fontSize: 12, fill: "#71717a" }} />
              <Tooltip
                labelFormatter={(d) => diaCurto.format(new Date(String(d)))}
                formatter={(v, nome) => [reais(Number(v) * 100), nome === "validada" ? "Validada" : "Ainda não validada"]}
              />
              <Bar dataKey="validada" stackId="c" fill="#ee4d2d" isAnimationActive={false} />
              <Bar
                dataKey={(p: { estimada: number; validada: number }) => p.estimada - p.validada}
                name="pendente"
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

function TabelaProdutos({
  workspaceId,
  produtos,
  className,
}: {
  workspaceId: string;
  produtos: Schemas["ResultadoProduto"][];
  className?: string;
}) {
  return (
    <Card className={cn("overflow-hidden", className)}>
      <h2 className="border-b border-borda px-4 py-3 font-medium">Por produto</h2>
      {produtos.length === 0 ? (
        <p className="p-6 text-center text-sm text-suave">Nenhuma venda no período.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-left text-xs text-suave">
              <tr>
                <th className="px-4 py-2 font-normal">Produto</th>
                <th className="px-2 py-2 text-right font-normal">Pedidos</th>
                <th className="px-2 py-2 text-right font-normal">Estimada</th>
                <th className="px-4 py-2 text-right font-normal">Validada</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-borda">
              {produtos.map((p) => (
                <tr key={p.item_id}>
                  <td className="px-4 py-2">
                    <div className="flex min-w-48 items-center gap-3">
                      <Imagem src={p.imagem_url} className="size-10 shrink-0 rounded-md" />
                      <span className="min-w-0">
                        {p.produto_id ? (
                          <Link
                            to="/w/$workspaceId/radar/$produtoId"
                            params={{ workspaceId, produtoId: p.produto_id }}
                            className="line-clamp-2 hover:underline"
                          >
                            {p.nome}
                          </Link>
                        ) : (
                          <span className="line-clamp-2">{p.nome}</span>
                        )}
                        <span className="block truncate text-xs text-suave">{p.loja_nome}</span>
                      </span>
                    </div>
                  </td>
                  <td className="px-2 py-2 text-right tabular-nums">{quantidade(p.pedidos)}</td>
                  <td className="px-2 py-2 text-right tabular-nums">{reais(p.comissao_estimada_centavos)}</td>
                  <td className="px-4 py-2 text-right tabular-nums">{reais(p.comissao_validada_centavos)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}

function TabelaCanais({ canais: linhas }: { canais: Schemas["ResultadoCanal"][] }) {
  return (
    <Card className="self-start overflow-hidden">
      <h2 className="border-b border-borda px-4 py-3 font-medium">Por canal</h2>
      {linhas.length === 0 ? (
        <p className="p-6 text-center text-sm text-suave">Nenhuma venda no período.</p>
      ) : (
        <ul className="divide-y divide-borda text-sm">
          {linhas.map((c) => (
            <li key={c.canal} className="flex items-center gap-2 px-4 py-2">
              <span className="flex-1">{canais[c.canal] ?? c.canal}</span>
              <span className="text-suave tabular-nums">{quantidade(c.pedidos)} ped.</span>
              <span className="w-24 text-right font-medium tabular-nums">{reais(c.comissao_estimada_centavos)}</span>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

function TabelaListas({ workspaceId, listas }: { workspaceId: string; listas: Schemas["ResultadoLista"][] }) {
  return (
    <Card className="overflow-hidden">
      <div className="border-b border-borda px-4 py-3">
        <h2 className="font-medium">Por lista</h2>
        <p className="text-xs text-suave">
          Vendas dos produtos de cada lista feitas por quem a importou, depois da importação.
        </p>
      </div>
      {listas.length === 0 ? (
        <p className="p-6 text-center text-sm text-suave">Nenhuma lista publicada ainda.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-left text-xs text-suave">
              <tr>
                <th className="px-4 py-2 font-normal">Lista</th>
                <th className="px-2 py-2 text-right font-normal">Importaram</th>
                <th className="px-2 py-2 text-right font-normal">Pedidos</th>
                <th className="px-2 py-2 text-right font-normal">Estimada</th>
                <th className="px-4 py-2 text-right font-normal">Validada</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-borda">
              {listas.map((l) => (
                <tr key={l.id}>
                  <td className="px-4 py-2">
                    <Link
                      to="/w/$workspaceId/listas/$listaId"
                      params={{ workspaceId, listaId: l.id }}
                      className="hover:underline"
                    >
                      {l.titulo}
                    </Link>
                  </td>
                  <td className="px-2 py-2 text-right tabular-nums">{quantidade(l.importadores)}</td>
                  <td className="px-2 py-2 text-right tabular-nums">{quantidade(l.pedidos)}</td>
                  <td className="px-2 py-2 text-right tabular-nums">{reais(l.comissao_estimada_centavos)}</td>
                  <td className="px-4 py-2 text-right tabular-nums">{reais(l.comissao_validada_centavos)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Card>
  );
}

function NotaCliques() {
  return (
    <p className="flex items-start gap-2 text-xs text-suave">
      <Info className="mt-0.5 size-3.5 shrink-0" />
      <span>
        Os números vêm do relatório de conversões da API oficial da Shopee. Ela não informa cliques; para vê-los, use o
        painel de afiliados da Shopee. A comissão validada soma os pedidos concluídos.
      </span>
    </p>
  );
}

