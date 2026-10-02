import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ArrowDown, ArrowUp, ChevronLeft, Copy, Download, Plus, Search, Send, Trash2, Users } from "lucide-react";
import { useEffect, useState } from "react";
import { api, exigir, type ItemLista, type ListaDetalhe } from "@/api/cliente";
import { Button } from "@/components/ui/button";
import { Aviso, Badge, Card } from "@/components/ui/card";
import { Input, Rotulo, Textarea } from "@/components/ui/input";
import { canais, copiar, textoParaCopiar } from "@/lib/copiar";
import { faixaPreco, haQuanto, porcentagem, reais } from "@/lib/formato";
import { useCanal, StatusLink } from "./colecao";
import { useInvalidarColecao } from "./colecao-api";
import { useConexaoShopee } from "./layout";
import { useAtualizarLista, useLista, useWorkspaceAtual } from "./listas-api";
import { Imagem } from "./radar";
import { rotaLista } from "./router";

type Caminho = { workspaceId: string; listaId: string };

export function Lista() {
  const { workspaceId, listaId } = rotaLista.useParams();
  const { gestor } = useWorkspaceAtual(workspaceId);
  const lista = useLista(workspaceId, listaId);
  const [selecionados, setSelecionados] = useState<Set<string>>(new Set());

  if (lista.error) {
    return (
      <div className="flex flex-col gap-4">
        <Voltar workspaceId={workspaceId} />
        <Aviso className="border-red-200 bg-red-50 text-red-800">{lista.error.message}</Aviso>
      </div>
    );
  }
  if (!lista.data) return <p className="text-sm text-suave">Carregando lista…</p>;
  const l = lista.data;
  const caminho = { workspaceId, listaId };
  const publicada = !!l.publicada_em;

  const alternar = (id: string) =>
    setSelecionados((s) => {
      const n = new Set(s);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });

  return (
    <div className="flex flex-col gap-5">
      <Voltar workspaceId={workspaceId} />
      {gestor ? <CabecalhoMentor lista={l} caminho={caminho} /> : <CabecalhoAfiliado lista={l} />}
      {gestor && publicada && <PainelImportacoes caminho={caminho} />}
      {publicada && (
        <Importar
          lista={l}
          caminho={caminho}
          selecionados={selecionados}
          onLimpar={() => setSelecionados(new Set())}
        />
      )}
      {gestor && l.itens.length < 100 && <AdicionarProduto caminho={caminho} />}

      {l.itens.length === 0 && (
        <Card className="p-8 text-center text-sm text-suave">
          {gestor ? "Lista vazia. Busque produtos do radar ou cole um link da Shopee acima." : "Esta lista está vazia."}
        </Card>
      )}
      <ul className="flex flex-col gap-3">
        {l.itens.map((it, i) => (
          <li key={it.produto.id}>
            <CartaoItemLista
              item={it}
              lista={l}
              indice={i}
              caminho={caminho}
              gestor={gestor}
              selecionado={selecionados.has(it.produto.id)}
              onSelecionar={publicada ? () => alternar(it.produto.id) : undefined}
            />
          </li>
        ))}
      </ul>
    </div>
  );
}

function Voltar({ workspaceId }: { workspaceId: string }) {
  return (
    <Link
      to="/w/$workspaceId/listas"
      params={{ workspaceId }}
      className="flex w-fit items-center gap-1 text-sm text-suave hover:text-zinc-900"
    >
      <ChevronLeft className="size-4" /> Listas
    </Link>
  );
}

function CabecalhoAfiliado({ lista: l }: { lista: ListaDetalhe }) {
  return (
    <div>
      <h1 className="text-2xl font-semibold">{l.titulo}</h1>
      {l.descricao && <p className="mt-1 whitespace-pre-line text-sm text-suave">{l.descricao}</p>}
      {l.publicada_em && <p className="mt-1 text-xs text-suave">Publicada {haQuanto(l.publicada_em)}</p>}
    </div>
  );
}

/** Mutação que devolve a lista atualizada. */
function useMudarLista(caminho: Caminho, fn: (c: Caminho) => Promise<ListaDetalhe>) {
  const atualizar = useAtualizarLista(caminho.workspaceId);
  return useMutation({ mutationFn: () => fn(caminho), onSuccess: (l) => atualizar(l) });
}

function CabecalhoMentor({ lista: l, caminho }: { lista: ListaDetalhe; caminho: Caminho }) {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const atualizar = useAtualizarLista(caminho.workspaceId);
  const [editando, setEditando] = useState(false);
  const [titulo, setTitulo] = useState(l.titulo);
  const [descricao, setDescricao] = useState(l.descricao);

  const salvar = useMutation({
    mutationFn: () =>
      exigir(
        api.PATCH("/v1/workspaces/{workspaceId}/listas/{listaId}", {
          params: { path: caminho },
          body: { titulo, descricao },
        }),
      ),
    onSuccess: (nova) => {
      atualizar(nova);
      setEditando(false);
    },
  });
  const publicar = useMudarLista(caminho, (c) =>
    exigir(api.POST("/v1/workspaces/{workspaceId}/listas/{listaId}/publicar", { params: { path: c } })),
  );
  const apagar = useMutation({
    mutationFn: () =>
      exigir(api.DELETE("/v1/workspaces/{workspaceId}/listas/{listaId}", { params: { path: caminho } })),
    onSuccess: async () => {
      qc.removeQueries({ queryKey: ["lista", caminho.workspaceId, caminho.listaId] });
      await qc.invalidateQueries({ queryKey: ["listas", caminho.workspaceId] });
      await navigate({ to: "/w/$workspaceId/listas", params: { workspaceId: caminho.workspaceId } });
    },
  });

  if (editando) {
    return (
      <Card className="p-4">
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            salvar.mutate();
          }}
        >
          <Rotulo texto="Título">
            <Input value={titulo} onChange={(e) => setTitulo(e.target.value)} maxLength={120} required />
          </Rotulo>
          <Rotulo texto="Descrição">
            <Textarea value={descricao} onChange={(e) => setDescricao(e.target.value)} maxLength={2000} />
          </Rotulo>
          {salvar.error && <p className="text-sm text-red-700">{salvar.error.message}</p>}
          <div className="flex gap-2">
            <Button type="submit" disabled={salvar.isPending}>
              Salvar
            </Button>
            <Button type="button" variante="fantasma" onClick={() => setEditando(false)}>
              Cancelar
            </Button>
          </div>
        </form>
      </Card>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <h1 className="text-2xl font-semibold">{l.titulo}</h1>
            {!l.publicada_em && <Badge className="bg-amber-50 text-amber-800">Rascunho</Badge>}
          </div>
          {l.descricao && <p className="mt-1 whitespace-pre-line text-sm text-suave">{l.descricao}</p>}
          <p className="mt-1 text-xs text-suave">
            {l.publicada_em ? `Publicada ${haQuanto(l.publicada_em)}` : "Só você e os mentores veem este rascunho."}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            variante="secundario"
            tamanho="sm"
            onClick={() => {
              setTitulo(l.titulo);
              setDescricao(l.descricao);
              setEditando(true);
            }}
          >
            Editar
          </Button>
          <Button
            variante="perigo"
            tamanho="sm"
            disabled={apagar.isPending}
            onClick={() => {
              const aviso = l.publicada_em
                ? "Apagar a lista? A turma deixa de vê-la, mas o que cada um importou continua na coleção."
                : "Apagar este rascunho?";
              if (window.confirm(aviso)) apagar.mutate();
            }}
          >
            <Trash2 className="size-4" /> Apagar
          </Button>
          {!l.publicada_em && (
            <Button
              tamanho="sm"
              disabled={publicar.isPending || l.itens.length === 0}
              title={l.itens.length === 0 ? "Adicione produtos antes de publicar" : undefined}
              onClick={() => {
                if (window.confirm("Publicar para a turma? Cada afiliado recebe um aviso no app, por e-mail e no navegador."))
                  publicar.mutate();
              }}
            >
              <Send className="size-4" /> {publicar.isPending ? "Publicando…" : "Publicar para a turma"}
            </Button>
          )}
        </div>
      </div>
      {(publicar.error || apagar.error) && (
        <Aviso className="border-red-200 bg-red-50 text-red-800">{(publicar.error ?? apagar.error)?.message}</Aviso>
      )}
      {publicar.isSuccess && (
        <Aviso className="border-emerald-200 bg-emerald-50 text-emerald-800">
          Lista publicada. A turma está sendo avisada.
        </Aviso>
      )}
    </div>
  );
}

function PainelImportacoes({ caminho }: { caminho: Caminho }) {
  const painel = useQuery({
    queryKey: ["painel-lista", caminho.workspaceId, caminho.listaId],
    queryFn: () =>
      exigir(api.GET("/v1/workspaces/{workspaceId}/listas/{listaId}/painel", { params: { path: caminho } })),
  });
  if (!painel.data) return null;
  const p = painel.data;
  return (
    <Card className="flex flex-col gap-2 p-4">
      <p className="flex items-center gap-2 text-sm font-medium">
        <Users className="size-4" />
        {p.importadores.length} de {p.afiliados} {p.afiliados === 1 ? "afiliado importou" : "afiliados importaram"}
      </p>
      {p.importadores.length > 0 && (
        <ul className="flex flex-wrap gap-1.5">
          {p.importadores.map((i) => (
            <li key={i.usuario_id}>
              <Badge className="bg-zinc-100 text-zinc-700" title={`Importou ${haQuanto(i.ultima_em)}`}>
                {i.nome} · {i.produtos === 1 ? "1 produto" : `${i.produtos} produtos`}
              </Badge>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

function Importar({
  lista: l,
  caminho,
  selecionados,
  onLimpar,
}: {
  lista: ListaDetalhe;
  caminho: Caminho;
  selecionados: Set<string>;
  onLimpar: () => void;
}) {
  const qc = useQueryClient();
  const invalidarColecao = useInvalidarColecao(caminho.workspaceId);
  const conexao = useConexaoShopee();
  const [colecao, setColecao] = useState(true);
  const faltam = l.itens.filter((i) => !i.meu_item).length;

  const importar = useMutation({
    mutationFn: (produtoIds: string[]) =>
      exigir(
        api.POST("/v1/workspaces/{workspaceId}/listas/{listaId}/importar", {
          params: { path: caminho },
          body: { produto_ids: produtoIds, colecao },
        }),
      ),
    onSuccess: () => {
      onLimpar();
      invalidarColecao();
      void qc.invalidateQueries({ queryKey: ["lista", caminho.workspaceId, caminho.listaId] });
      void qc.invalidateQueries({ queryKey: ["listas", caminho.workspaceId] });
      void qc.invalidateQueries({ queryKey: ["painel-lista", caminho.workspaceId, caminho.listaId] });
    },
  });
  const r = importar.data;

  return (
    <Card className="flex flex-col gap-3 p-4">
      <div className="flex flex-wrap items-center gap-2">
        <Button
          disabled={importar.isPending || (selecionados.size === 0 && faltam === 0)}
          onClick={() => importar.mutate([...selecionados])}
        >
          <Download className="size-4" />
          {importar.isPending
            ? "Importando…"
            : selecionados.size > 0
              ? `Importar ${selecionados.size === 1 ? "1 selecionado" : `${selecionados.size} selecionados`}`
              : faltam === 0
                ? "Tudo já está na sua coleção"
                : "Importar a lista para a minha coleção"}
        </Button>
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={colecao} onChange={(e) => setColecao(e.target.checked)} />
          Guardar na coleção "{l.titulo.slice(0, 60)}"
        </label>
      </div>
      <p className="text-xs text-suave">
        {selecionados.size > 0
          ? "Só os produtos marcados entram na sua coleção."
          : "Marque produtos para importar só parte da lista. As dicas do mentor vão para as suas notas."}
      </p>
      {importar.error && <p className="text-sm text-red-700">{importar.error.message}</p>}
      {r && (
        <Aviso
          className={
            r.link_status === "gerando"
              ? "border-emerald-200 bg-emerald-50 text-emerald-800"
              : "border-amber-200 bg-amber-50 text-amber-800"
          }
        >
          {r.criados === 0
            ? "Esses produtos já estavam na sua coleção."
            : `${r.criados === 1 ? "1 produto salvo" : `${r.criados} produtos salvos`} na sua coleção.`}
          {r.ja_salvos > 0 && r.criados > 0 && ` ${r.ja_salvos} já estavam lá.`}{" "}
          {r.criados > 0 && r.link_status === "gerando" && "Os seus links estão sendo gerados com a sua conta da Shopee."}
          {r.criados > 0 && r.link_status === "pendente" && (
            <>
              Os links ficam pendentes até você{" "}
              <Link to="/conta/shopee" className="font-medium underline">
                conectar a sua conta da Shopee
              </Link>
              .
            </>
          )}
          {r.criados > 0 && r.link_status === "falhou" && "Não deu para gerar os links agora. Tente de novo pela coleção."}
        </Aviso>
      )}
      {!r && conexao.data && conexao.data.status !== "conectado" && (
        <p className="text-xs text-amber-800">
          Sua conta da Shopee não está conectada: os produtos entram na coleção, mas os links só saem depois que você{" "}
          <Link to="/conta/shopee" className="underline">
            conectar
          </Link>
          .
        </p>
      )}
    </Card>
  );
}

function CartaoItemLista({
  item: it,
  lista,
  indice,
  caminho,
  gestor,
  selecionado,
  onSelecionar,
}: {
  item: ItemLista;
  lista: ListaDetalhe;
  indice: number;
  caminho: Caminho;
  gestor: boolean;
  selecionado: boolean;
  onSelecionar?: () => void;
}) {
  const p = it.produto;
  return (
    <Card className="flex flex-col gap-3 p-3 sm:flex-row">
      <div className="flex min-w-0 flex-1 gap-3">
        {onSelecionar && (
          <input
            type="checkbox"
            className="mt-1 size-4 shrink-0"
            aria-label={`Selecionar ${p.nome}`}
            checked={selecionado}
            onChange={onSelecionar}
          />
        )}
        <a href={p.url} target="_blank" rel="noreferrer" className="shrink-0">
          <Imagem src={p.imagem_url} className="size-20 rounded-lg" />
        </a>
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <p className="line-clamp-2 text-sm font-medium leading-snug">{p.nome}</p>
          <p className="text-sm">
            {faixaPreco(p.preco_min_centavos, p.preco_max_centavos)}{" "}
            <span className="text-xs text-suave">· comissão {porcentagem(p.comissao_bp)}</span>
          </p>
          <p className="text-xs text-emerald-700">Ganha {reais(p.ganho_por_venda_centavos)} por venda</p>
          {gestor ? (
            <Comentario item={it} caminho={caminho} />
          ) : (
            it.comentario && (
              <p className="mt-1 rounded-lg bg-orange-50 px-3 py-2 text-sm text-orange-900">
                <span className="font-medium">Dica do mentor:</span> {it.comentario}
              </p>
            )
          )}
        </div>
      </div>
      <div className="flex shrink-0 flex-col items-stretch gap-2 sm:w-52">
        {gestor && <ControlesMentor item={it} lista={lista} indice={indice} caminho={caminho} />}
        {it.meu_item ? <MeuLink item={it} workspaceId={caminho.workspaceId} /> : null}
      </div>
    </Card>
  );
}

function Comentario({ item: it, caminho }: { item: ItemLista; caminho: Caminho }) {
  const atualizar = useAtualizarLista(caminho.workspaceId);
  const [texto, setTexto] = useState(it.comentario);
  useEffect(() => setTexto(it.comentario), [it.comentario]);
  const salvar = useMutation({
    mutationFn: () =>
      exigir(
        api.PATCH("/v1/workspaces/{workspaceId}/listas/{listaId}/itens/{produtoId}", {
          params: { path: { ...caminho, produtoId: it.produto.id } },
          body: { comentario: texto },
        }),
      ),
    onSuccess: (l) => atualizar(l),
  });
  return (
    <div className="mt-1 flex flex-col gap-1">
      <Textarea
        aria-label="Dica para a turma"
        className="min-h-14 text-sm"
        placeholder="Dica para a turma (ex.: vende muito no reels)"
        value={texto}
        maxLength={1000}
        onChange={(e) => setTexto(e.target.value)}
        onBlur={() => texto !== it.comentario && salvar.mutate()}
      />
      {salvar.isPending && <span className="text-xs text-suave">Salvando…</span>}
      {salvar.error && <span className="text-xs text-red-700">{salvar.error.message}</span>}
    </div>
  );
}

function ControlesMentor({
  item: it,
  lista,
  indice,
  caminho,
}: {
  item: ItemLista;
  lista: ListaDetalhe;
  indice: number;
  caminho: Caminho;
}) {
  const atualizar = useAtualizarLista(caminho.workspaceId);
  const mover = useMutation({
    mutationFn: (destino: number) => {
      const ids = lista.itens.map((i) => i.produto.id);
      ids.splice(destino, 0, ...ids.splice(indice, 1));
      return exigir(
        api.PUT("/v1/workspaces/{workspaceId}/listas/{listaId}/ordem", {
          params: { path: caminho },
          body: { produto_ids: ids },
        }),
      );
    },
    onSuccess: (l) => atualizar(l),
  });
  const remover = useMutation({
    mutationFn: () =>
      exigir(
        api.DELETE("/v1/workspaces/{workspaceId}/listas/{listaId}/itens/{produtoId}", {
          params: { path: { ...caminho, produtoId: it.produto.id } },
        }),
      ),
    onSuccess: (l) => atualizar(l),
  });
  return (
    <div className="flex items-center justify-end gap-1">
      {it.importadores !== undefined && lista.publicada_em && (
        <span className="mr-auto text-xs text-suave">
          {it.importadores === 1 ? "1 importou" : `${it.importadores} importaram`}
        </span>
      )}
      <Button
        variante="fantasma"
        tamanho="sm"
        aria-label="Subir"
        disabled={indice === 0 || mover.isPending}
        onClick={() => mover.mutate(indice - 1)}
      >
        <ArrowUp className="size-4" />
      </Button>
      <Button
        variante="fantasma"
        tamanho="sm"
        aria-label="Descer"
        disabled={indice === lista.itens.length - 1 || mover.isPending}
        onClick={() => mover.mutate(indice + 1)}
      >
        <ArrowDown className="size-4" />
      </Button>
      <Button
        variante="fantasma"
        tamanho="sm"
        aria-label="Tirar da lista"
        disabled={remover.isPending}
        onClick={() => remover.mutate()}
      >
        <Trash2 className="size-4" />
      </Button>
    </div>
  );
}

/** O link de afiliado de quem vê, para produtos já na coleção dele. */
function MeuLink({ item: it, workspaceId }: { item: ItemLista; workspaceId: string }) {
  const [canal] = useCanal();
  const [copiado, setCopiado] = useState<boolean | null>(null);
  const meu = it.meu_item!;
  const pronto = meu.link_status === "pronto";
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center justify-between gap-2">
        <StatusLink item={meu} />
        <Link
          to="/w/$workspaceId/colecao/$itemId"
          params={{ workspaceId, itemId: meu.id }}
          className="text-xs text-suave hover:underline"
        >
          Na sua coleção
        </Link>
      </div>
      {pronto && (
        <Button
          tamanho="sm"
          variante="secundario"
          onClick={async () => setCopiado(await copiar(textoParaCopiar({ ...meu, produto: it.produto }, canal)))}
          onBlur={() => setCopiado(null)}
        >
          <Copy className="size-4" />
          {copiado === true ? "Copiado!" : copiado === false ? "Não deu para copiar" : `Copiar para ${canais[canal]}`}
        </Button>
      )}
    </div>
  );
}

function AdicionarProduto({ caminho }: { caminho: Caminho }) {
  const atualizar = useAtualizarLista(caminho.workspaceId);
  const [texto, setTexto] = useState("");
  const [busca, setBusca] = useState("");
  const ehLink = /^(https?:\/\/)?([a-z0-9-]+\.)*shopee\.com\.br\//i.test(texto.trim());

  useEffect(() => {
    const t = setTimeout(() => setBusca(ehLink ? "" : texto.trim()), 300);
    return () => clearTimeout(t);
  }, [texto, ehLink]);

  const resultados = useQuery({
    queryKey: ["radar", caminho.workspaceId, { q: busca, por_pagina: 6 }],
    enabled: busca.length >= 2,
    queryFn: () =>
      exigir(
        api.GET("/v1/workspaces/{workspaceId}/radar", {
          params: { path: { workspaceId: caminho.workspaceId }, query: { q: busca, por_pagina: 6 } },
        }),
      ),
  });
  const adicionar = useMutation({
    mutationFn: (corpo: { produto_id: string } | { url: string }) =>
      exigir(
        api.POST("/v1/workspaces/{workspaceId}/listas/{listaId}/itens", { params: { path: caminho }, body: corpo }),
      ),
    onSuccess: (l) => {
      atualizar(l);
      setTexto("");
    },
  });

  return (
    <Card className="flex flex-col gap-3 p-4">
      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          if (ehLink) adicionar.mutate({ url: texto.trim() });
        }}
      >
        <div className="relative flex-1">
          <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-suave" />
          <Input
            aria-label="Adicionar produto"
            className="pl-9"
            placeholder="Busque no radar ou cole o link de um produto da Shopee"
            value={texto}
            onChange={(e) => {
              setTexto(e.target.value);
              adicionar.reset();
            }}
          />
        </div>
        {ehLink && (
          <Button type="submit" disabled={adicionar.isPending}>
            <Plus className="size-4" /> {adicionar.isPending ? "Adicionando…" : "Adicionar"}
          </Button>
        )}
      </form>
      {adicionar.error && <p className="text-sm text-red-700">{adicionar.error.message}</p>}
      {busca.length >= 2 && resultados.data && (
        <ul className="flex flex-col divide-y divide-borda">
          {resultados.data.itens.length === 0 && (
            <li className="py-2 text-sm text-suave">Nada no radar com esse nome. Cole o link do produto.</li>
          )}
          {resultados.data.itens.map((r) => (
            <li key={r.produto_id} className="flex items-center gap-3 py-2">
              <Imagem src={r.imagem_url} className="size-10 shrink-0 rounded" />
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm">{r.nome}</p>
                <p className="text-xs text-suave">
                  {reais(r.preco_min_centavos)} · comissão {porcentagem(r.comissao_bp)}
                </p>
              </div>
              <Button
                tamanho="sm"
                variante="secundario"
                disabled={adicionar.isPending}
                onClick={() => adicionar.mutate({ produto_id: r.produto_id })}
              >
                <Plus className="size-4" /> Adicionar
              </Button>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}
