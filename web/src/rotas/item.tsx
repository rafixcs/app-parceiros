import { useMutation, useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ArrowLeft, Copy, ExternalLink, RefreshCw, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { api, exigir, type Item as ItemAPI } from "@/api/cliente";
import type { paths } from "@/api/schema";
import { Button } from "@/components/ui/button";
import { Aviso, Card } from "@/components/ui/card";
import { Input, Rotulo, Select, Textarea } from "@/components/ui/input";
import { type Canal, canais, copiar, linkDoCanal, textoParaCopiar } from "@/lib/copiar";
import { faixaPreco, porcentagem, reais } from "@/lib/formato";
import { useColecoes, useInvalidarColecao } from "./colecao-api";
import { gerando, StatusLink, statusItem, useCanal } from "./colecao";
import { Imagem } from "./radar";
import { rotaItem } from "./router";

type Edicao = paths["/v1/workspaces/{workspaceId}/itens/{itemId}"]["patch"]["requestBody"]["content"]["application/json"];

export function Item() {
  const { workspaceId, itemId } = rotaItem.useParams();
  const navigate = useNavigate();
  const invalidar = useInvalidarColecao(workspaceId);
  const caminho = { params: { path: { workspaceId, itemId } } };

  const item = useQuery({
    queryKey: ["item", workspaceId, itemId],
    queryFn: () => exigir(api.GET("/v1/workspaces/{workspaceId}/itens/{itemId}", caminho)),
    refetchInterval: (q) => gerando(q.state.data ? [q.state.data] : undefined),
  });
  const editar = useMutation({
    mutationFn: (body: Edicao) => exigir(api.PATCH("/v1/workspaces/{workspaceId}/itens/{itemId}", { ...caminho, body })),
    onSuccess: invalidar,
  });
  const remover = useMutation({
    mutationFn: () => exigir(api.DELETE("/v1/workspaces/{workspaceId}/itens/{itemId}", caminho)),
    onSuccess: async () => {
      invalidar();
      await navigate({ to: "/w/$workspaceId/colecao", params: { workspaceId }, search: {} });
    },
  });

  const voltar = (
    <Link
      to="/w/$workspaceId/colecao"
      params={{ workspaceId }}
      search={{}}
      className="inline-flex items-center gap-1 text-sm text-suave hover:text-texto"
    >
      <ArrowLeft className="size-4" /> Minha coleção
    </Link>
  );
  if (item.error) {
    return (
      <div className="flex flex-col gap-4">
        {voltar}
        <Aviso className="border-red-200 bg-red-50 text-red-800">{item.error.message}</Aviso>
      </div>
    );
  }
  if (item.isPending) return <p className="text-sm text-suave">Carregando…</p>;
  const it = item.data;
  const p = it.produto;

  return (
    <div className="flex flex-col gap-4">
      {voltar}
      <Card className="flex gap-4 p-4">
        <Imagem src={p.imagem_url} className="size-24 shrink-0 rounded-lg sm:size-32" />
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <p className="text-sm text-suave">{p.loja_nome}</p>
          <p className="font-medium leading-snug">{p.nome}</p>
          <p className="text-sm">
            {faixaPreco(p.preco_min_centavos, p.preco_max_centavos)} · comissão {porcentagem(p.comissao_bp)} ·{" "}
            <span className="font-medium text-emerald-700">ganha {reais(p.ganho_por_venda_centavos)} por venda</span>
          </p>
          <a
            href={p.url}
            target="_blank"
            rel="noreferrer"
            className="mt-auto inline-flex items-center gap-1 text-sm text-marca hover:underline"
          >
            Ver na Shopee <ExternalLink className="size-3.5" />
          </a>
        </div>
      </Card>

      <div className="grid gap-4 lg:grid-cols-[1fr_22rem]">
        <FormItem item={it} salvando={editar.isPending} erro={editar.error?.message} onSalvar={(b) => editar.mutate(b)} />
        <div className="flex flex-col gap-4">
          <CopiarRapido item={it} />
          <LinkAfiliado item={it} workspaceId={workspaceId} />
          <ColecoesDoItem item={it} workspaceId={workspaceId} />
          <Button
            variante="perigo"
            disabled={remover.isPending}
            onClick={() => {
              if (confirm("Remover este produto da sua coleção? As notas e os links dele serão apagados.")) remover.mutate();
            }}
          >
            <Trash2 className="size-4" /> Remover da coleção
          </Button>
        </div>
      </div>
    </div>
  );
}

function FormItem({
  item,
  salvando,
  erro,
  onSalvar,
}: {
  item: ItemAPI;
  salvando: boolean;
  erro?: string;
  onSalvar: (b: Edicao) => void;
}) {
  const [titulo, setTitulo] = useState(item.titulo);
  const [descricao, setDescricao] = useState(item.descricao);
  const [notas, setNotas] = useState(item.notas);
  const [tags, setTags] = useState(item.tags.join(", "));
  const [status, setStatus] = useState(item.status);
  // Recarrega o formulário quando outro item é aberto.
  useEffect(() => {
    setTitulo(item.titulo);
    setDescricao(item.descricao);
    setNotas(item.notas);
    setTags(item.tags.join(", "));
    setStatus(item.status);
  }, [item.id]); // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <Card className="p-4">
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          onSalvar({
            titulo,
            descricao,
            notas,
            status,
            tags: tags
              .split(",")
              .map((t) => t.trim())
              .filter(Boolean),
          });
        }}
      >
        <Rotulo texto="Título para divulgar">
          <Input value={titulo} maxLength={200} onChange={(e) => setTitulo(e.target.value)} placeholder={item.produto.nome} />
        </Rotulo>
        <Rotulo texto="Descrição">
          <Textarea
            rows={4}
            maxLength={2000}
            value={descricao}
            onChange={(e) => setDescricao(e.target.value)}
            placeholder="O texto que vai junto do link nas redes sociais"
          />
        </Rotulo>
        <Rotulo texto="Notas (só você vê)">
          <Textarea rows={3} maxLength={5000} value={notas} onChange={(e) => setNotas(e.target.value)} />
        </Rotulo>
        <div className="grid grid-cols-2 gap-3">
          <Rotulo texto="Tags, separadas por vírgula">
            <Input value={tags} onChange={(e) => setTags(e.target.value)} placeholder="casa, presente" />
          </Rotulo>
          <Rotulo texto="Status">
            <Select value={status} onChange={(e) => setStatus(e.target.value as ItemAPI["status"])}>
              {Object.entries(statusItem).map(([v, s]) => (
                <option key={v} value={v}>
                  {s.rotulo}
                </option>
              ))}
            </Select>
          </Rotulo>
        </div>
        {erro && <Aviso className="border-red-200 bg-red-50 text-red-800">{erro}</Aviso>}
        <Button type="submit" disabled={salvando} className="self-start">
          {salvando ? "Salvando…" : "Salvar alterações"}
        </Button>
      </form>
    </Card>
  );
}

function CopiarRapido({ item }: { item: ItemAPI }) {
  const [canal, setCanal] = useCanal();
  const [copiado, setCopiado] = useState<boolean | null>(null);
  const texto = textoParaCopiar(item, canal);
  return (
    <Card className="flex flex-col gap-3 p-4">
      <h2 className="font-medium">Copiar rápido</h2>
      <Select aria-label="Canal" value={canal} onChange={(e) => setCanal(e.target.value as Canal)}>
        {Object.entries(canais).map(([v, rotulo]) => (
          <option key={v} value={v}>
            {rotulo}
          </option>
        ))}
      </Select>
      <pre className="max-h-40 overflow-auto rounded-lg bg-zinc-50 p-3 text-xs whitespace-pre-wrap">{texto}</pre>
      {!linkDoCanal(item, canal) && (
        <p className="text-xs text-amber-800">O link ainda não está pronto; o texto vai sem ele.</p>
      )}
      <Button onClick={async () => setCopiado(await copiar(texto))} onBlur={() => setCopiado(null)}>
        <Copy className="size-4" />
        {copiado === true ? "Copiado!" : copiado === false ? "Não deu para copiar" : "Copiar título, descrição e link"}
      </Button>
    </Card>
  );
}

function LinkAfiliado({ item, workspaceId }: { item: ItemAPI; workspaceId: string }) {
  const invalidar = useInvalidarColecao(workspaceId);
  const caminho = { params: { path: { workspaceId, itemId: item.id } } };
  const [manual, setManual] = useState("");
  const definir = useMutation({
    mutationFn: (link: string | null) =>
      exigir(api.PATCH("/v1/workspaces/{workspaceId}/itens/{itemId}", { ...caminho, body: { link_afiliado: link } })),
    onSuccess: (it) => {
      setManual("");
      invalidar(it);
    },
  });
  const regerar = useMutation({
    mutationFn: () => exigir(api.POST("/v1/workspaces/{workspaceId}/itens/{itemId}/link", caminho)),
    onSuccess: invalidar,
  });

  return (
    <Card className="flex flex-col gap-3 p-4">
      <div className="flex items-center gap-2">
        <h2 className="mr-auto font-medium">Link de afiliado</h2>
        <StatusLink item={item} />
      </div>
      {item.link_status === "pendente" && (
        <p className="text-sm text-suave">
          <Link to="/conta/shopee" className="text-marca hover:underline">
            Conecte a sua conta da Shopee
          </Link>{" "}
          para gerar o link com o seu ID de afiliado.
        </p>
      )}
      {item.link_status === "falhou" && (
        <p className="text-sm text-suave">A Shopee não gerou o link. Tente de novo em instantes.</p>
      )}
      {item.link_origem === "manual" && item.link_afiliado && (
        <p className="truncate text-sm">
          Usando o seu link: <span className="font-mono text-xs">{item.link_afiliado}</span>
        </p>
      )}
      {item.links.length > 0 && (
        <ul className="flex flex-col gap-1.5">
          {item.links.map((l) => (
            <li key={l.canal} className="flex items-center gap-2 text-sm">
              <span className="w-20 shrink-0 text-suave">{canais[l.canal]}</span>
              <span className="min-w-0 flex-1 truncate font-mono text-xs" title={`subId ${l.sub_id}`}>
                {l.url}
              </span>
              <Button variante="fantasma" tamanho="sm" aria-label={`Copiar link do ${canais[l.canal]}`} onClick={() => copiar(l.url)}>
                <Copy className="size-4" />
              </Button>
            </li>
          ))}
        </ul>
      )}
      <Button
        variante="secundario"
        tamanho="sm"
        disabled={regerar.isPending || item.link_status === "gerando"}
        onClick={() => regerar.mutate()}
      >
        <RefreshCw className="size-4" />
        {item.link_origem === "manual" ? "Voltar ao link automático" : "Gerar de novo"}
      </Button>
      <form
        className="flex flex-col gap-2 border-t border-borda pt-3"
        onSubmit={(e) => {
          e.preventDefault();
          definir.mutate(manual.trim());
        }}
      >
        <Rotulo texto="Ou use um link seu">
          <Input
            type="url"
            placeholder="https://s.shopee.com.br/..."
            value={manual}
            onChange={(e) => setManual(e.target.value)}
            required
          />
        </Rotulo>
        {definir.error && <p className="text-sm text-red-700">{definir.error.message}</p>}
        <Button type="submit" tamanho="sm" variante="secundario" disabled={definir.isPending || !manual.trim()}>
          Usar este link
        </Button>
      </form>
    </Card>
  );
}

function ColecoesDoItem({ item, workspaceId }: { item: ItemAPI; workspaceId: string }) {
  const colecoes = useColecoes(workspaceId);
  const invalidar = useInvalidarColecao(workspaceId);
  const definir = useMutation({
    mutationFn: (ids: string[]) =>
      exigir(
        api.PUT("/v1/workspaces/{workspaceId}/itens/{itemId}/colecoes", {
          params: { path: { workspaceId, itemId: item.id } },
          body: { colecao_ids: ids },
        }),
      ),
    onSuccess: invalidar,
  });
  const marcadas = new Set(item.colecao_ids);

  return (
    <Card className="flex flex-col gap-2 p-4">
      <h2 className="font-medium">Coleções</h2>
      {colecoes.data?.length === 0 && (
        <p className="text-sm text-suave">Crie coleções na página da sua coleção para organizar os produtos.</p>
      )}
      {colecoes.data?.map((c) => (
        <label key={c.id} className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            className="size-4 accent-marca"
            checked={marcadas.has(c.id)}
            disabled={definir.isPending}
            onChange={(e) => {
              const ids = new Set(marcadas);
              if (e.target.checked) ids.add(c.id);
              else ids.delete(c.id);
              definir.mutate([...ids]);
            }}
          />
          {c.nome}
        </label>
      ))}
      {definir.error && <p className="text-sm text-red-700">{definir.error.message}</p>}
    </Card>
  );
}
