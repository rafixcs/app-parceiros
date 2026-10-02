import { keepPreviousData, useMutation, useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ChevronLeft, ChevronRight, Copy, Link2, Plus, Search } from "lucide-react";
import { useEffect, useState } from "react";
import { api, exigir, type Item } from "@/api/cliente";
import { Button } from "@/components/ui/button";
import { Aviso, Badge, Card } from "@/components/ui/card";
import { Input, Select } from "@/components/ui/input";
import { type Canal, canais, copiar, textoParaCopiar } from "@/lib/copiar";
import { faixaPreco, reais } from "@/lib/formato";
import { cn } from "@/lib/utils";
import { useColecoes, useInvalidarColecao, useSalvar } from "./colecao-api";
import { useConexaoShopee } from "./layout";
import { Imagem } from "./radar";
import { rotaColecao } from "./router";

export const statusItem = {
  testando: { rotulo: "Testando", classe: "bg-sky-50 text-sky-800" },
  campeao: { rotulo: "Campeão", classe: "bg-amber-50 text-amber-800" },
  descartado: { rotulo: "Descartado", classe: "bg-zinc-100 text-zinc-600" },
} as const;
type StatusItem = keyof typeof statusItem;

export type BuscaColecao = {
  q?: string;
  status?: StatusItem;
  colecao?: string;
  tag?: string;
  pagina?: number;
};

const POR_PAGINA = 30;

export function validarBuscaColecao(s: Record<string, unknown>): BuscaColecao {
  const texto = (v: unknown, max: number) => (typeof v === "string" && v ? v.slice(0, max) : undefined);
  const pagina = Number(s.pagina);
  return {
    q: texto(s.q, 100),
    status: typeof s.status === "string" && s.status in statusItem ? (s.status as StatusItem) : undefined,
    colecao: texto(s.colecao, 36),
    tag: texto(s.tag, 30),
    pagina: Number.isInteger(pagina) && pagina > 1 ? pagina : undefined,
  };
}

const chaveCanal = "parceiros.canal";

/** Canal escolhido no "copiar rápido", lembrado entre as visitas. */
export function useCanal(): [Canal, (c: Canal) => void] {
  const [canal, setCanal] = useState<Canal>(() => {
    try {
      const c = localStorage.getItem(chaveCanal);
      return c && c in canais ? (c as Canal) : "instagram";
    } catch {
      return "instagram";
    }
  });
  return [
    canal,
    (c) => {
      setCanal(c);
      try {
        localStorage.setItem(chaveCanal, c);
      } catch {
        // Sem localStorage, só não lembra a escolha.
      }
    },
  ];
}

/** Repete a consulta enquanto algum link está sendo gerado. */
export function gerando(itens: Item[] | undefined) {
  return itens?.some((i) => i.link_status === "gerando") ? 2500 : false;
}

export function Colecao() {
  const { workspaceId } = rotaColecao.useParams();
  const busca = rotaColecao.useSearch();
  const navigate = useNavigate({ from: rotaColecao.fullPath });
  const pagina = busca.pagina ?? 1;
  const [canal, setCanal] = useCanal();

  const itens = useQuery({
    queryKey: ["itens", workspaceId, busca],
    queryFn: () =>
      exigir(
        api.GET("/v1/workspaces/{workspaceId}/itens", {
          params: { path: { workspaceId }, query: { ...busca, pagina, por_pagina: POR_PAGINA } },
        }),
      ),
    placeholderData: keepPreviousData,
    refetchInterval: (q) => gerando(q.state.data?.itens),
  });
  const colecoes = useColecoes(workspaceId);
  const conexao = useConexaoShopee();
  const invalidar = useInvalidarColecao(workspaceId);
  const pendentes = useMutation({
    mutationFn: () =>
      exigir(
        api.POST("/v1/workspaces/{workspaceId}/itens/links-pendentes", { params: { path: { workspaceId } } }),
      ),
    onSuccess: () => invalidar(),
  });

  const filtrar = (m: Partial<BuscaColecao>) => navigate({ search: (a) => ({ ...a, ...m, pagina: undefined }) });
  const total = itens.data?.total ?? 0;
  const paginas = Math.max(1, Math.ceil(total / POR_PAGINA));
  const conectado = conexao.data?.status === "conectado";
  const semLink = itens.data?.itens.some((i) => i.link_status === "pendente" || i.link_status === "falhou");
  const vazia = itens.data && total === 0 && !busca.q && !busca.status && !busca.colecao && !busca.tag;

  return (
    <div className="flex flex-col gap-5">
      <div>
        <h1 className="text-2xl font-semibold">Minha coleção</h1>
        <p className="text-sm text-suave">
          Os produtos que você vai divulgar, com o seu link de afiliado. Só você vê esta coleção.
        </p>
      </div>

      {conexao.data && !conectado && (
        <Aviso className="border-amber-200 bg-amber-50 text-amber-900">
          Os seus links ficam pendentes até você{" "}
          <Link to="/conta/shopee" className="font-medium underline">
            conectar a sua conta de afiliado da Shopee
          </Link>
          .
        </Aviso>
      )}
      {conectado && semLink && (
        <Aviso className="flex flex-wrap items-center gap-3 border-amber-200 bg-amber-50 text-amber-900">
          <span className="flex-1">Alguns produtos ainda estão sem link de afiliado.</span>
          <Button tamanho="sm" disabled={pendentes.isPending} onClick={() => pendentes.mutate()}>
            Gerar links pendentes
          </Button>
        </Aviso>
      )}

      <ColarLink workspaceId={workspaceId} />

      <BarraColecoes
        workspaceId={workspaceId}
        atual={busca.colecao}
        colecoes={colecoes.data ?? []}
        onEscolher={(colecao) => filtrar({ colecao })}
      />

      <Filtros busca={busca} onFiltrar={filtrar} canal={canal} onCanal={setCanal} />

      {itens.error && <Aviso className="border-red-200 bg-red-50 text-red-800">{itens.error.message}</Aviso>}
      {itens.isPending && <p className="text-sm text-suave">Carregando…</p>}
      {vazia && (
        <Card className="p-8 text-center text-sm text-suave">
          Você ainda não salvou nenhum produto. Salve pelo{" "}
          <Link to="/w/$workspaceId/radar" params={{ workspaceId }} search={{}} className="text-marca hover:underline">
            radar
          </Link>{" "}
          ou cole o link de um produto da Shopee acima.
        </Card>
      )}
      {itens.data && total === 0 && !vazia && (
        <Card className="p-8 text-center text-sm text-suave">Nenhum produto com esses filtros.</Card>
      )}

      <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {itens.data?.itens.map((it) => (
          <li key={it.id}>
            <CartaoItem item={it} workspaceId={workspaceId} canal={canal} onTag={(tag) => filtrar({ tag })} />
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
            Página {pagina} de {paginas}
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

function ColarLink({ workspaceId }: { workspaceId: string }) {
  const navigate = useNavigate();
  const [url, setUrl] = useState("");
  const salvar = useSalvar(workspaceId);
  return (
    <Card className="p-3">
      <form
        className="flex flex-col gap-2 sm:flex-row"
        onSubmit={(e) => {
          e.preventDefault();
          salvar.mutate(
            { url: url.trim() },
            {
              onSuccess: (item) => {
                setUrl("");
                void navigate({ to: "/w/$workspaceId/colecao/$itemId", params: { workspaceId, itemId: item.id } });
              },
            },
          );
        }}
      >
        <div className="relative flex-1">
          <Link2 className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-suave" />
          <Input
            aria-label="Link do produto na Shopee"
            className="pl-9"
            placeholder="Cole o link de um produto da Shopee"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            required
          />
        </div>
        <Button type="submit" disabled={salvar.isPending || !url.trim()}>
          {salvar.isPending ? "Buscando…" : "Salvar produto"}
        </Button>
      </form>
      {salvar.error && <p className="mt-2 text-sm text-red-700">{salvar.error.message}</p>}
    </Card>
  );
}

function BarraColecoes({
  workspaceId,
  atual,
  colecoes,
  onEscolher,
}: {
  workspaceId: string;
  atual?: string;
  colecoes: { id: string; nome: string; itens: number }[];
  onEscolher: (id?: string) => void;
}) {
  const [nova, setNova] = useState<string | null>(null);
  const invalidar = useInvalidarColecao(workspaceId);
  const criar = useMutation({
    mutationFn: (nome: string) =>
      exigir(api.POST("/v1/workspaces/{workspaceId}/colecoes", { params: { path: { workspaceId } }, body: { nome } })),
    onSuccess: () => {
      setNova(null);
      invalidar();
    },
  });
  const chip = (ativo: boolean) =>
    cn(
      "h-8 rounded-full border px-3 text-sm",
      ativo ? "border-marca bg-marca text-white" : "border-borda bg-white hover:bg-zinc-50",
    );

  return (
    <div className="flex flex-wrap items-center gap-2">
      <button type="button" className={chip(!atual)} onClick={() => onEscolher(undefined)}>
        Todos
      </button>
      {colecoes.map((c) => (
        <button key={c.id} type="button" className={chip(atual === c.id)} onClick={() => onEscolher(c.id)}>
          {c.nome} <span className="opacity-70">{c.itens}</span>
        </button>
      ))}
      {nova === null ? (
        <Button variante="fantasma" tamanho="sm" onClick={() => setNova("")}>
          <Plus className="size-4" /> Nova coleção
        </Button>
      ) : (
        <form
          className="flex items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            criar.mutate(nova);
          }}
        >
          <Input
            autoFocus
            aria-label="Nome da coleção"
            className="h-8 w-48"
            maxLength={60}
            placeholder="Ex.: Achados da semana"
            value={nova}
            onChange={(e) => setNova(e.target.value)}
            onKeyDown={(e) => e.key === "Escape" && setNova(null)}
          />
          <Button tamanho="sm" type="submit" disabled={criar.isPending || !nova.trim()}>
            Criar
          </Button>
          {criar.error && <span className="text-sm text-red-700">{criar.error.message}</span>}
        </form>
      )}
    </div>
  );
}

function Filtros({
  busca,
  onFiltrar,
  canal,
  onCanal,
}: {
  busca: BuscaColecao;
  onFiltrar: (m: Partial<BuscaColecao>) => void;
  canal: Canal;
  onCanal: (c: Canal) => void;
}) {
  const [q, setQ] = useState(busca.q ?? "");
  useEffect(() => {
    const t = setTimeout(() => {
      if ((busca.q ?? "") !== q.trim()) onFiltrar({ q: q.trim() || undefined });
    }, 350);
    return () => clearTimeout(t);
  }, [q]); // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
      <div className="relative col-span-2">
        <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-suave" />
        <Input
          aria-label="Buscar na coleção"
          className="pl-9"
          placeholder="Buscar no título, descrição ou notas"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
      </div>
      <Select
        aria-label="Status"
        value={busca.status ?? ""}
        onChange={(e) => onFiltrar({ status: (e.target.value || undefined) as StatusItem | undefined })}
      >
        <option value="">Todos os status</option>
        {Object.entries(statusItem).map(([v, s]) => (
          <option key={v} value={v}>
            {s.rotulo}
          </option>
        ))}
      </Select>
      <Select aria-label="Canal do link ao copiar" value={canal} onChange={(e) => onCanal(e.target.value as Canal)}>
        {Object.entries(canais).map(([v, rotulo]) => (
          <option key={v} value={v}>
            Copiar para {rotulo}
          </option>
        ))}
      </Select>
      {busca.tag && (
        <div className="col-span-2 flex items-center gap-2 text-sm md:col-span-4">
          Tag: <Badge className="bg-zinc-100 text-zinc-700">{busca.tag}</Badge>
          <button type="button" className="text-suave hover:underline" onClick={() => onFiltrar({ tag: undefined })}>
            limpar
          </button>
        </div>
      )}
    </div>
  );
}

export function StatusLink({ item }: { item: Pick<Item, "link_status" | "link_origem"> }) {
  switch (item.link_status) {
    case "pronto":
      return item.link_origem === "manual" ? (
        <Badge className="bg-emerald-50 text-emerald-800">Link manual</Badge>
      ) : (
        <Badge className="bg-emerald-50 text-emerald-800">Link pronto</Badge>
      );
    case "gerando":
      return <Badge className="bg-sky-50 text-sky-800">Gerando link…</Badge>;
    case "pendente":
      return (
        <Badge className="bg-amber-50 text-amber-800" title="Conecte a sua conta da Shopee para gerar o link">
          Link pendente
        </Badge>
      );
    case "falhou":
      return <Badge className="bg-red-50 text-red-700">Link falhou</Badge>;
  }
}

function CartaoItem({
  item: it,
  workspaceId,
  canal,
  onTag,
}: {
  item: Item;
  workspaceId: string;
  canal: Canal;
  onTag: (t: string) => void;
}) {
  const [copiado, setCopiado] = useState<boolean | null>(null);
  const s = statusItem[it.status];
  return (
    <Card className="flex h-full flex-col gap-2 p-3">
      <Link
        to="/w/$workspaceId/colecao/$itemId"
        params={{ workspaceId, itemId: it.id }}
        className="flex gap-3 hover:opacity-90"
      >
        <Imagem src={it.produto.imagem_url} className="size-20 shrink-0 rounded-lg" />
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <p className="line-clamp-2 text-sm font-medium leading-snug">{it.titulo || it.produto.nome}</p>
          <p className="text-sm">{faixaPreco(it.produto.preco_min_centavos, it.produto.preco_max_centavos)}</p>
          <p className="text-xs text-emerald-700">Ganha {reais(it.produto.ganho_por_venda_centavos)} por venda</p>
        </div>
      </Link>
      <div className="flex flex-wrap items-center gap-1.5">
        <Badge className={s.classe}>{s.rotulo}</Badge>
        <StatusLink item={it} />
        {it.tags.map((t) => (
          <button key={t} type="button" onClick={() => onTag(t)}>
            <Badge className="bg-zinc-100 text-zinc-700 hover:bg-zinc-200">#{t}</Badge>
          </button>
        ))}
      </div>
      <Button
        tamanho="sm"
        variante="secundario"
        className="mt-auto"
        onClick={async () => setCopiado(await copiar(textoParaCopiar(it, canal)))}
        onBlur={() => setCopiado(null)}
      >
        <Copy className="size-4" />
        {copiado === true ? "Copiado!" : copiado === false ? "Não deu para copiar" : `Copiar para ${canais[canal]}`}
      </Button>
    </Card>
  );
}
