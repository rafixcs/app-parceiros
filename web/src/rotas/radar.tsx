import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ChevronLeft, ChevronRight, Flame, Search, Star } from "lucide-react";
import { useEffect, useState } from "react";
import { api, exigir, type RadarItem } from "@/api/cliente";
import { Button } from "@/components/ui/button";
import { Aviso, Badge, Card } from "@/components/ui/card";
import { Input, Rotulo, Select } from "@/components/ui/input";
import { faixaPreco, haQuanto, paraCentavos, porcentagem, quantidade, reais } from "@/lib/formato";
import { rotaRadar } from "./router";

const ordens = {
  tendencia: "Em alta",
  ganho: "Maior ganho por venda",
  comissao: "Maior comissão",
  vendas: "Mais vendidos",
} as const;
type Ordem = keyof typeof ordens;

export type BuscaRadar = {
  q?: string;
  categoria?: number;
  preco_min?: number;
  preco_max?: number;
  comissao_min?: number;
  nota_min?: number;
  ordem?: Ordem;
  pagina?: number;
};

const POR_PAGINA = 24;

function numero(v: unknown): number | undefined {
  const n = typeof v === "number" ? v : typeof v === "string" && v !== "" ? Number(v) : NaN;
  return Number.isFinite(n) && n >= 0 ? n : undefined;
}

/** Os filtros vivem na URL, para o link do radar filtrado poder ser compartilhado. */
export function validarBusca(s: Record<string, unknown>): BuscaRadar {
  const ordem = typeof s.ordem === "string" && s.ordem in ordens ? (s.ordem as Ordem) : undefined;
  const pagina = numero(s.pagina);
  return {
    q: typeof s.q === "string" && s.q ? s.q.slice(0, 100) : undefined,
    categoria: numero(s.categoria),
    preco_min: numero(s.preco_min),
    preco_max: numero(s.preco_max),
    comissao_min: numero(s.comissao_min),
    nota_min: numero(s.nota_min),
    ordem,
    pagina: pagina && pagina > 1 ? Math.floor(pagina) : undefined,
  };
}

export function Radar() {
  const { workspaceId } = rotaRadar.useParams();
  const busca = rotaRadar.useSearch();
  const navigate = useNavigate({ from: rotaRadar.fullPath });
  const pagina = busca.pagina ?? 1;

  const radar = useQuery({
    queryKey: ["radar", workspaceId, busca],
    queryFn: () =>
      exigir(
        api.GET("/v1/workspaces/{workspaceId}/radar", {
          params: { path: { workspaceId }, query: { ...busca, pagina, por_pagina: POR_PAGINA } },
        }),
      ),
    placeholderData: keepPreviousData,
  });
  const categorias = useQuery({
    queryKey: ["radar-categorias", workspaceId],
    queryFn: () =>
      exigir(api.GET("/v1/workspaces/{workspaceId}/radar/categorias", { params: { path: { workspaceId } } })),
    staleTime: 10 * 60_000,
  });

  const filtrar = (mudanca: Partial<BuscaRadar>) =>
    navigate({ search: (atual) => ({ ...atual, ...mudanca, pagina: undefined }) });

  const total = radar.data?.total ?? 0;
  const paginas = Math.max(1, Math.ceil(total / POR_PAGINA));

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <h1 className="text-2xl font-semibold">Radar</h1>
          <p className="text-sm text-suave">
            Produtos da Shopee em alta, com quanto você ganha por venda.
            {radar.data?.atualizado_em && <> Atualizado {haQuanto(radar.data.atualizado_em)}.</>}
          </p>
        </div>
      </div>

      <Filtros busca={busca} categorias={categorias.data ?? []} onFiltrar={filtrar} />

      {radar.error && (
        <Aviso className="border-red-200 bg-red-50 text-red-800">{radar.error.message}</Aviso>
      )}
      {radar.isPending && <p className="text-sm text-suave">Carregando produtos…</p>}
      {radar.data && radar.data.itens.length === 0 && (
        <Card className="p-8 text-center text-sm text-suave">
          {radar.data.atualizado_em
            ? "Nenhum produto com esses filtros. Tente afrouxar algum."
            : "O radar ainda não tem produtos. A primeira coleta da Shopee roda logo depois que o worker sobe."}
        </Card>
      )}

      <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {radar.data?.itens.map((p) => (
          <li key={p.produto_id}>
            <CartaoProduto produto={p} workspaceId={workspaceId} />
          </li>
        ))}
      </ul>

      {total > POR_PAGINA && (
        <nav className="flex items-center justify-center gap-3 text-sm" aria-label="Páginas">
          <Button
            variante="secundario"
            tamanho="sm"
            disabled={pagina <= 1}
            onClick={() => navigate({ search: (a) => ({ ...a, pagina: pagina - 1 }) })}
          >
            <ChevronLeft className="size-4" /> Anterior
          </Button>
          <span className="text-suave">
            Página {pagina} de {paginas} · {quantidade(total)} produtos
          </span>
          <Button
            variante="secundario"
            tamanho="sm"
            disabled={pagina >= paginas}
            onClick={() => navigate({ search: (a) => ({ ...a, pagina: pagina + 1 }) })}
          >
            Próxima <ChevronRight className="size-4" />
          </Button>
        </nav>
      )}
    </div>
  );
}

function Filtros({
  busca,
  categorias,
  onFiltrar,
}: {
  busca: BuscaRadar;
  categorias: { id: number; nome: string }[];
  onFiltrar: (m: Partial<BuscaRadar>) => void;
}) {
  const [q, setQ] = useState(busca.q ?? "");
  const [precoMin, setPrecoMin] = useState(busca.preco_min ? String(busca.preco_min / 100) : "");
  const [precoMax, setPrecoMax] = useState(busca.preco_max ? String(busca.preco_max / 100) : "");

  // A busca por texto filtra enquanto a pessoa digita, com uma pausa.
  useEffect(() => {
    const t = setTimeout(() => {
      if ((busca.q ?? "") !== q.trim()) onFiltrar({ q: q.trim() || undefined });
    }, 350);
    return () => clearTimeout(t);
  }, [q]); // eslint-disable-line react-hooks/exhaustive-deps

  const aplicarPreco = () => {
    const min = paraCentavos(precoMin);
    const max = paraCentavos(precoMax);
    if (min !== busca.preco_min || max !== busca.preco_max) onFiltrar({ preco_min: min, preco_max: max });
  };
  const enterAplica = (e: React.KeyboardEvent) => e.key === "Enter" && aplicarPreco();

  return (
    <Card className="grid grid-cols-2 gap-3 p-3 md:grid-cols-6">
      <div className="relative col-span-2">
        <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-suave" />
        <Input
          aria-label="Buscar produto ou loja"
          className="pl-9"
          placeholder="Buscar produto ou loja"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
      </div>
      <Select
        aria-label="Categoria"
        value={busca.categoria ?? ""}
        onChange={(e) => onFiltrar({ categoria: e.target.value ? Number(e.target.value) : undefined })}
      >
        <option value="">Todas as categorias</option>
        {categorias.map((c) => (
          <option key={c.id} value={c.id}>
            {c.nome}
          </option>
        ))}
      </Select>
      <Select
        aria-label="Ordenar por"
        value={busca.ordem ?? "tendencia"}
        onChange={(e) => onFiltrar({ ordem: e.target.value === "tendencia" ? undefined : (e.target.value as Ordem) })}
      >
        {Object.entries(ordens).map(([v, rotulo]) => (
          <option key={v} value={v}>
            {rotulo}
          </option>
        ))}
      </Select>
      <Select
        aria-label="Comissão mínima"
        value={busca.comissao_min ?? ""}
        onChange={(e) => onFiltrar({ comissao_min: e.target.value ? Number(e.target.value) : undefined })}
      >
        <option value="">Qualquer comissão</option>
        {[500, 1000, 1500, 2000].map((bp) => (
          <option key={bp} value={bp}>
            A partir de {porcentagem(bp)}
          </option>
        ))}
      </Select>
      <Select
        aria-label="Nota mínima"
        value={busca.nota_min ?? ""}
        onChange={(e) => onFiltrar({ nota_min: e.target.value ? Number(e.target.value) : undefined })}
      >
        <option value="">Qualquer nota</option>
        {[4, 4.5, 4.8].map((n) => (
          <option key={n} value={n}>
            {n.toLocaleString("pt-BR")} estrelas ou mais
          </option>
        ))}
      </Select>
      <div className="col-span-2 flex items-end gap-2 md:col-span-6">
        <Rotulo texto="Preço de (R$)">
          <Input inputMode="decimal" value={precoMin} onChange={(e) => setPrecoMin(e.target.value)} onBlur={aplicarPreco} onKeyDown={enterAplica} />
        </Rotulo>
        <Rotulo texto="até (R$)">
          <Input inputMode="decimal" value={precoMax} onChange={(e) => setPrecoMax(e.target.value)} onBlur={aplicarPreco} onKeyDown={enterAplica} />
        </Rotulo>
      </div>
    </Card>
  );
}

function CartaoProduto({ produto: p, workspaceId }: { produto: RadarItem; workspaceId: string }) {
  return (
    <Link
      to="/w/$workspaceId/radar/$produtoId"
      params={{ workspaceId, produtoId: p.produto_id }}
      className="group block h-full"
    >
      <Card className="flex h-full gap-3 p-3 transition-shadow group-hover:shadow-md">
        <Imagem src={p.imagem_url} className="size-24 shrink-0 rounded-lg" />
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <p className="line-clamp-2 text-sm font-medium leading-snug">{p.nome}</p>
          <p className="truncate text-xs text-suave">{p.loja_nome}</p>
          <p className="text-sm">{faixaPreco(p.preco_min_centavos, p.preco_max_centavos)}</p>
          <div className="mt-auto flex flex-wrap items-center gap-1.5">
            <Badge className="bg-emerald-50 text-emerald-800" title="Comissão × preço mínimo">
              Ganha {reais(p.ganho_por_venda_centavos)}
            </Badge>
            <Badge className="bg-zinc-100 text-zinc-700">{porcentagem(p.comissao_bp)}</Badge>
            <Tendencia produto={p} />
          </div>
          <p className="flex items-center gap-2 text-xs text-suave">
            {quantidade(p.vendas)} vendidos
            {p.nota != null && (
              <span className="flex items-center gap-0.5">
                <Star className="size-3 fill-amber-400 text-amber-400" />
                {p.nota.toLocaleString("pt-BR")}
              </span>
            )}
          </p>
        </div>
      </Card>
    </Link>
  );
}

export function Tendencia({ produto: p }: { produto: RadarItem }) {
  if (p.vendas_7d == null) {
    return (
      <Badge className="bg-zinc-100 text-suave" title="Ainda não há um dia de histórico para medir o crescimento">
        Sem histórico
      </Badge>
    );
  }
  return (
    <Badge className="bg-orange-50 text-marca-escura" title={`+${quantidade(p.vendas_7d)} vendas em 7 dias`}>
      <Flame className="size-3" />
      {Math.round(p.score)}
    </Badge>
  );
}

export function Imagem({ src, className }: { src: string | null; className?: string }) {
  const [falhou, setFalhou] = useState(false);
  if (!src || falhou) return <div className={`bg-zinc-100 ${className ?? ""}`} aria-hidden />;
  return (
    <img
      src={src}
      alt=""
      loading="lazy"
      referrerPolicy="no-referrer"
      onError={() => setFalhou(true)}
      className={`object-cover ${className ?? ""}`}
    />
  );
}
