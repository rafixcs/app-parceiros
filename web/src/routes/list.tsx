import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ArrowDown, ArrowUp, ChevronLeft, Clapperboard, Copy, Download, Plus, Search, Send, Trash2, Unlink, Users } from "lucide-react";
import { useEffect, useState } from "react";
import { api, unwrap, type CuratedListDetail, type CuratedListItem } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Notice, Badge, Card } from "@/components/ui/card";
import { Input, Field, Select, Textarea } from "@/components/ui/input";
import { AddVideo } from "@/components/video/add";
import { VideoCaption, VideoPlayer } from "@/components/video/player";
import { channels, copyText, textToCopy } from "@/lib/clipboard";
import { money, percent, priceRange, timeAgo } from "@/lib/format";
import { LinkStatus, useChannel } from "./collection";
import { useInvalidateCollection } from "./collection-api";
import { useCurrentWorkspace, useShopeeConnection } from "./layout";
import { useList, useUpdateList } from "./lists-api";
import { ProductImage } from "./radar";
import { listRoute } from "./router";
import { VideoCard } from "./videos";
import { useListVideo, useVideos } from "./videos-api";

type ListPath = { workspaceId: string; listId: string };

export function ListPage() {
  const { workspaceId, listId } = listRoute.useParams();
  const { manager } = useCurrentWorkspace(workspaceId);
  const list = useList(workspaceId, listId);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  if (list.error) {
    return (
      <div className="flex flex-col gap-4">
        <Back workspaceId={workspaceId} />
        <Notice className="border-red-200 bg-red-50 text-red-800">{list.error.message}</Notice>
      </div>
    );
  }
  if (!list.data) return <p className="text-sm text-muted">Carregando lista…</p>;
  const l = list.data;
  const path = { workspaceId, listId };
  const published = !!l.published_at;

  const toggle = (id: string) =>
    setSelected((s) => {
      const n = new Set(s);
      if (n.has(id)) n.delete(id);
      else n.add(id);
      return n;
    });

  return (
    <div className="flex flex-col gap-5">
      <Back workspaceId={workspaceId} />
      {manager ? <MentorHeader list={l} path={path} /> : <AffiliateHeader list={l} />}
      {manager && published && <ImportDashboard path={path} />}
      {published && (
        <Import list={l} path={path} selected={selected} onClear={() => setSelected(new Set())} />
      )}
      {(manager || l.videos.length > 0) && <ListVideos list={l} path={path} manager={manager} />}
      {manager && l.items.length < 100 && <AddProduct path={path} />}

      {l.items.length === 0 && (
        <Card className="p-8 text-center text-sm text-muted">
          {manager ? "Lista vazia. Busque produtos do radar ou cole um link da Shopee acima." : "Esta lista está vazia."}
        </Card>
      )}
      <ul className="flex flex-col gap-3">
        {l.items.map((it, i) => (
          <li key={it.product.id}>
            <ListItemCard
              item={it}
              list={l}
              index={i}
              path={path}
              manager={manager}
              selected={selected.has(it.product.id)}
              onSelect={published ? () => toggle(it.product.id) : undefined}
            />
          </li>
        ))}
      </ul>
    </div>
  );
}

function Back({ workspaceId }: { workspaceId: string }) {
  return (
    <Link
      to="/w/$workspaceId/listas"
      params={{ workspaceId }}
      className="flex w-fit items-center gap-1 text-sm text-muted hover:text-zinc-900"
    >
      <ChevronLeft className="size-4" /> Listas
    </Link>
  );
}

function AffiliateHeader({ list: l }: { list: CuratedListDetail }) {
  return (
    <div>
      <h1 className="text-2xl font-semibold">{l.title}</h1>
      {l.description && <p className="mt-1 whitespace-pre-line text-sm text-muted">{l.description}</p>}
      {l.published_at && <p className="mt-1 text-xs text-muted">Publicada {timeAgo(l.published_at)}</p>}
    </div>
  );
}

/** A mutation that returns the updated list. */
function useChangeList(path: ListPath, fn: (p: ListPath) => Promise<CuratedListDetail>) {
  const update = useUpdateList(path.workspaceId);
  return useMutation({ mutationFn: () => fn(path), onSuccess: (l) => update(l) });
}

function MentorHeader({ list: l, path }: { list: CuratedListDetail; path: ListPath }) {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const update = useUpdateList(path.workspaceId);
  const [editing, setEditing] = useState(false);
  const [title, setTitle] = useState(l.title);
  const [description, setDescription] = useState(l.description);

  const save = useMutation({
    mutationFn: () =>
      unwrap(
        api.PATCH("/v1/workspaces/{workspaceId}/lists/{listId}", {
          params: { path },
          body: { title, description },
        }),
      ),
    onSuccess: (updated) => {
      update(updated);
      setEditing(false);
    },
  });
  const publish = useChangeList(path, (p) =>
    unwrap(api.POST("/v1/workspaces/{workspaceId}/lists/{listId}/publish", { params: { path: p } })),
  );
  const remove = useMutation({
    mutationFn: () => unwrap(api.DELETE("/v1/workspaces/{workspaceId}/lists/{listId}", { params: { path } })),
    onSuccess: async () => {
      qc.removeQueries({ queryKey: ["list", path.workspaceId, path.listId] });
      await qc.invalidateQueries({ queryKey: ["lists", path.workspaceId] });
      await navigate({ to: "/w/$workspaceId/listas", params: { workspaceId: path.workspaceId } });
    },
  });

  if (editing) {
    return (
      <Card className="p-4">
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <Field label="Título">
            <Input value={title} onChange={(e) => setTitle(e.target.value)} maxLength={120} required />
          </Field>
          <Field label="Descrição">
            <Textarea value={description} onChange={(e) => setDescription(e.target.value)} maxLength={2000} />
          </Field>
          {save.error && <p className="text-sm text-red-700">{save.error.message}</p>}
          <div className="flex gap-2">
            <Button type="submit" disabled={save.isPending}>
              Salvar
            </Button>
            <Button type="button" variant="ghost" onClick={() => setEditing(false)}>
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
            <h1 className="text-2xl font-semibold">{l.title}</h1>
            {!l.published_at && <Badge className="bg-amber-50 text-amber-800">Rascunho</Badge>}
          </div>
          {l.description && <p className="mt-1 whitespace-pre-line text-sm text-muted">{l.description}</p>}
          <p className="mt-1 text-xs text-muted">
            {l.published_at ? `Publicada ${timeAgo(l.published_at)}` : "Só você e os mentores veem este rascunho."}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="secondary"
            size="sm"
            onClick={() => {
              setTitle(l.title);
              setDescription(l.description);
              setEditing(true);
            }}
          >
            Editar
          </Button>
          <Button
            variant="danger"
            size="sm"
            disabled={remove.isPending}
            onClick={() => {
              const warning = l.published_at
                ? "Apagar a lista? A turma deixa de vê-la, mas o que cada um importou continua na coleção."
                : "Apagar este rascunho?";
              if (window.confirm(warning)) remove.mutate();
            }}
          >
            <Trash2 className="size-4" /> Apagar
          </Button>
          {!l.published_at && (
            <Button
              size="sm"
              disabled={publish.isPending || l.items.length === 0}
              title={l.items.length === 0 ? "Adicione produtos antes de publicar" : undefined}
              onClick={() => {
                if (window.confirm("Publicar para a turma? Cada afiliado recebe um aviso no app, por e-mail e no navegador."))
                  publish.mutate();
              }}
            >
              <Send className="size-4" /> {publish.isPending ? "Publicando…" : "Publicar para a turma"}
            </Button>
          )}
        </div>
      </div>
      {(publish.error || remove.error) && (
        <Notice className="border-red-200 bg-red-50 text-red-800">{(publish.error ?? remove.error)?.message}</Notice>
      )}
      {publish.isSuccess && (
        <Notice className="border-emerald-200 bg-emerald-50 text-emerald-800">
          Lista publicada. A turma está sendo avisada.
        </Notice>
      )}
    </div>
  );
}

function ImportDashboard({ path }: { path: ListPath }) {
  const dashboard = useQuery({
    queryKey: ["list-dashboard", path.workspaceId, path.listId],
    queryFn: () => unwrap(api.GET("/v1/workspaces/{workspaceId}/lists/{listId}/dashboard", { params: { path } })),
  });
  if (!dashboard.data) return null;
  const d = dashboard.data;
  return (
    <Card className="flex flex-col gap-2 p-4">
      <p className="flex items-center gap-2 text-sm font-medium">
        <Users className="size-4" />
        {d.importers.length} de {d.affiliates} {d.affiliates === 1 ? "afiliado importou" : "afiliados importaram"}
      </p>
      {d.importers.length > 0 && (
        <ul className="flex flex-wrap gap-1.5">
          {d.importers.map((i) => (
            <li key={i.user_id}>
              <Badge className="bg-zinc-100 text-zinc-700" title={`Importou ${timeAgo(i.last_imported_at)}`}>
                {i.name} · {i.products === 1 ? "1 produto" : `${i.products} produtos`}
              </Badge>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

function Import({
  list: l,
  path,
  selected,
  onClear,
}: {
  list: CuratedListDetail;
  path: ListPath;
  selected: Set<string>;
  onClear: () => void;
}) {
  const qc = useQueryClient();
  const invalidateCollection = useInvalidateCollection(path.workspaceId);
  const connection = useShopeeConnection();
  const [collection, setCollection] = useState(true);
  const missing = l.items.filter((i) => !i.my_item).length;

  const importList = useMutation({
    mutationFn: (productIds: string[]) =>
      unwrap(
        api.POST("/v1/workspaces/{workspaceId}/lists/{listId}/import", {
          params: { path },
          body: { product_ids: productIds, collection },
        }),
      ),
    onSuccess: () => {
      onClear();
      invalidateCollection();
      void qc.invalidateQueries({ queryKey: ["list", path.workspaceId, path.listId] });
      void qc.invalidateQueries({ queryKey: ["lists", path.workspaceId] });
      void qc.invalidateQueries({ queryKey: ["list-dashboard", path.workspaceId, path.listId] });
    },
  });
  const r = importList.data;

  return (
    <Card className="flex flex-col gap-3 p-4">
      <div className="flex flex-wrap items-center gap-2">
        <Button
          disabled={importList.isPending || (selected.size === 0 && missing === 0)}
          onClick={() => importList.mutate([...selected])}
        >
          <Download className="size-4" />
          {importList.isPending
            ? "Importando…"
            : selected.size > 0
              ? `Importar ${selected.size === 1 ? "1 selecionado" : `${selected.size} selecionados`}`
              : missing === 0
                ? "Tudo já está na sua coleção"
                : "Importar a lista para a minha coleção"}
        </Button>
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={collection} onChange={(e) => setCollection(e.target.checked)} />
          Guardar na coleção "{l.title.slice(0, 60)}"
        </label>
      </div>
      <p className="text-xs text-muted">
        {selected.size > 0
          ? "Só os produtos marcados entram na sua coleção."
          : "Marque produtos para importar só parte da lista. As dicas do mentor vão para as suas notas."}
      </p>
      {importList.error && <p className="text-sm text-red-700">{importList.error.message}</p>}
      {r && (
        <Notice
          className={
            r.link_status === "generating"
              ? "border-emerald-200 bg-emerald-50 text-emerald-800"
              : "border-amber-200 bg-amber-50 text-amber-800"
          }
        >
          {r.created === 0
            ? "Esses produtos já estavam na sua coleção."
            : `${r.created === 1 ? "1 produto salvo" : `${r.created} produtos salvos`} na sua coleção.`}
          {r.already_saved > 0 && r.created > 0 && ` ${r.already_saved} já estavam lá.`}{" "}
          {r.created > 0 && r.link_status === "generating" && "Os seus links estão sendo gerados com a sua conta da Shopee."}
          {r.created > 0 && r.link_status === "pending" && (
            <>
              Os links ficam pendentes até você{" "}
              <Link to="/conta/shopee" className="font-medium underline">
                conectar a sua conta da Shopee
              </Link>
              .
            </>
          )}
          {r.created > 0 && r.link_status === "failed" && "Não deu para gerar os links agora. Tente de novo pela coleção."}
        </Notice>
      )}
      {!r && connection.data && connection.data.status !== "connected" && (
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

function ListItemCard({
  item: it,
  list,
  index,
  path,
  manager,
  selected,
  onSelect,
}: {
  item: CuratedListItem;
  list: CuratedListDetail;
  index: number;
  path: ListPath;
  manager: boolean;
  selected: boolean;
  onSelect?: () => void;
}) {
  const p = it.product;
  return (
    <Card className="flex flex-col gap-3 p-3 sm:flex-row sm:flex-wrap">
      <div className="flex min-w-0 flex-1 gap-3">
        {onSelect && (
          <input
            type="checkbox"
            className="mt-1 size-4 shrink-0"
            aria-label={`Selecionar ${p.name}`}
            checked={selected}
            onChange={onSelect}
          />
        )}
        <a href={p.url} target="_blank" rel="noreferrer" className="shrink-0">
          <ProductImage src={p.image_url} className="size-20 rounded-lg" />
        </a>
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <p className="line-clamp-2 text-sm font-medium leading-snug">{p.name}</p>
          <p className="text-sm">
            {priceRange(p.min_price_cents, p.max_price_cents)}{" "}
            <span className="text-xs text-muted">· comissão {percent(p.commission_bp)}</span>
          </p>
          <p className="text-xs text-emerald-700">Ganha {money(p.earnings_per_sale_cents)} por venda</p>
          {manager ? (
            <Comment item={it} path={path} />
          ) : (
            it.comment && (
              <p className="mt-1 rounded-lg bg-orange-50 px-3 py-2 text-sm text-orange-900">
                <span className="font-medium">Dica do mentor:</span> {it.comment}
              </p>
            )
          )}
        </div>
      </div>
      <div className="flex shrink-0 flex-col items-stretch gap-2 sm:w-52">
        {manager && <MentorControls item={it} list={list} index={index} path={path} />}
        {it.my_item ? <MyLink item={it} workspaceId={path.workspaceId} /> : null}
      </div>
      {it.videos.length > 0 && (
        <ul className="flex basis-full gap-3 overflow-x-auto pb-1" aria-label={`Vídeos de ${p.name}`}>
          {it.videos.map((v) => (
            <li key={v.id} className="flex w-40 shrink-0 flex-col gap-1">
              <VideoPlayer video={v} />
              <VideoCaption video={v} />
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

/** Videos attached to the list: the mentor attaches one from the library or adds a new one. */
function ListVideos({ list: l, path, manager }: { list: CuratedListDetail; path: ListPath; manager: boolean }) {
  const { workspaceId, listId } = path;
  const attach = useListVideo(workspaceId, listId);
  const library = useVideos(workspaceId);
  const [adding, setAdding] = useState(false);
  const [chosen, setChosen] = useState("");
  const attached = new Set(l.videos.map((v) => v.id));
  const available = (library.data ?? []).filter((v) => v.mine && !attached.has(v.id));

  return (
    <Card className="flex flex-col gap-3 p-4">
      <div className="flex items-center gap-2">
        <Clapperboard className="size-4 text-muted" />
        <h2 className="mr-auto font-medium">Vídeos da lista</h2>
        {manager && (
          <Button size="sm" variant="secondary" onClick={() => setAdding((a) => !a)}>
            <Plus className="size-4" /> Anexar vídeo
          </Button>
        )}
      </div>
      {manager && adding && (
        <div className="flex flex-col gap-3 rounded-lg border border-border p-3">
          {available.length > 0 && (
            <form
              className="flex gap-2"
              onSubmit={(e) => {
                e.preventDefault();
                attach.mutate({ id: chosen, attached: true }, { onSuccess: () => setChosen("") });
              }}
            >
              <Select aria-label="Vídeo da biblioteca" value={chosen} onChange={(e) => setChosen(e.target.value)}>
                <option value="">Escolha um vídeo da sua biblioteca…</option>
                {available.map((v) => (
                  <option key={v.id} value={v.id}>
                    {v.title || "Sem título"}
                  </option>
                ))}
              </Select>
              <Button type="submit" variant="secondary" disabled={!chosen || attach.isPending}>
                Anexar
              </Button>
            </form>
          )}
          <AddVideo
            workspaceId={workspaceId}
            onAdded={(v) => attach.mutate({ id: v.id, attached: true }, { onSuccess: () => setAdding(false) })}
          />
          <p className="text-xs text-muted">Os vídeos anexados ficam visíveis para toda a turma.</p>
        </div>
      )}
      {attach.error && <p className="text-sm text-red-700">{attach.error.message}</p>}
      {l.videos.length === 0 ? (
        <p className="text-sm text-muted">Anexe vídeos de referência ou seus para a turma se inspirar.</p>
      ) : (
        <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {l.videos.map((v) => (
            <li key={v.id}>
              <VideoCard
                video={v}
                workspaceId={workspaceId}
                canShare={manager}
                actions={
                  manager && (
                    <Button
                      variant="ghost"
                      size="sm"
                      title="Tirar da lista"
                      disabled={attach.isPending}
                      onClick={() => attach.mutate({ id: v.id, attached: false })}
                    >
                      <Unlink className="size-4" />
                      <span className="sr-only">Tirar da lista</span>
                    </Button>
                  )
                }
              />
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

function Comment({ item: it, path }: { item: CuratedListItem; path: ListPath }) {
  const update = useUpdateList(path.workspaceId);
  const [text, setText] = useState(it.comment);
  useEffect(() => setText(it.comment), [it.comment]);
  const save = useMutation({
    mutationFn: () =>
      unwrap(
        api.PATCH("/v1/workspaces/{workspaceId}/lists/{listId}/items/{productId}", {
          params: { path: { ...path, productId: it.product.id } },
          body: { comment: text },
        }),
      ),
    onSuccess: (l) => update(l),
  });
  return (
    <div className="mt-1 flex flex-col gap-1">
      <Textarea
        aria-label="Dica para a turma"
        className="min-h-14 text-sm"
        placeholder="Dica para a turma (ex.: vende muito no reels)"
        value={text}
        maxLength={1000}
        onChange={(e) => setText(e.target.value)}
        onBlur={() => text !== it.comment && save.mutate()}
      />
      {save.isPending && <span className="text-xs text-muted">Salvando…</span>}
      {save.error && <span className="text-xs text-red-700">{save.error.message}</span>}
    </div>
  );
}

function MentorControls({
  item: it,
  list,
  index,
  path,
}: {
  item: CuratedListItem;
  list: CuratedListDetail;
  index: number;
  path: ListPath;
}) {
  const update = useUpdateList(path.workspaceId);
  const move = useMutation({
    mutationFn: (target: number) => {
      const ids = list.items.map((i) => i.product.id);
      ids.splice(target, 0, ...ids.splice(index, 1));
      return unwrap(
        api.PUT("/v1/workspaces/{workspaceId}/lists/{listId}/order", {
          params: { path },
          body: { product_ids: ids },
        }),
      );
    },
    onSuccess: (l) => update(l),
  });
  const remove = useMutation({
    mutationFn: () =>
      unwrap(
        api.DELETE("/v1/workspaces/{workspaceId}/lists/{listId}/items/{productId}", {
          params: { path: { ...path, productId: it.product.id } },
        }),
      ),
    onSuccess: (l) => update(l),
  });
  return (
    <div className="flex items-center justify-end gap-1">
      {it.importers !== undefined && list.published_at && (
        <span className="mr-auto text-xs text-muted">
          {it.importers === 1 ? "1 importou" : `${it.importers} importaram`}
        </span>
      )}
      <Button
        variant="ghost"
        size="sm"
        aria-label="Subir"
        disabled={index === 0 || move.isPending}
        onClick={() => move.mutate(index - 1)}
      >
        <ArrowUp className="size-4" />
      </Button>
      <Button
        variant="ghost"
        size="sm"
        aria-label="Descer"
        disabled={index === list.items.length - 1 || move.isPending}
        onClick={() => move.mutate(index + 1)}
      >
        <ArrowDown className="size-4" />
      </Button>
      <Button
        variant="ghost"
        size="sm"
        aria-label="Tirar da lista"
        disabled={remove.isPending}
        onClick={() => remove.mutate()}
      >
        <Trash2 className="size-4" />
      </Button>
    </div>
  );
}

/** The viewer's affiliate link, for products already in their collection. */
function MyLink({ item: it, workspaceId }: { item: CuratedListItem; workspaceId: string }) {
  const [channel] = useChannel();
  const [copied, setCopied] = useState<boolean | null>(null);
  const mine = it.my_item!;
  const ready = mine.link_status === "ready";
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center justify-between gap-2">
        <LinkStatus item={mine} />
        <Link
          to="/w/$workspaceId/colecao/$itemId"
          params={{ workspaceId, itemId: mine.id }}
          className="text-xs text-muted hover:underline"
        >
          Na sua coleção
        </Link>
      </div>
      {ready && (
        <Button
          size="sm"
          variant="secondary"
          onClick={async () => setCopied(await copyText(textToCopy({ ...mine, product: it.product }, channel)))}
          onBlur={() => setCopied(null)}
        >
          <Copy className="size-4" />
          {copied === true ? "Copiado!" : copied === false ? "Não deu para copiar" : `Copiar para ${channels[channel]}`}
        </Button>
      )}
    </div>
  );
}

function AddProduct({ path }: { path: ListPath }) {
  const update = useUpdateList(path.workspaceId);
  const [text, setText] = useState("");
  const [query, setQuery] = useState("");
  const isLink = /^(https?:\/\/)?([a-z0-9-]+\.)*shopee\.com\.br\//i.test(text.trim());

  useEffect(() => {
    const t = setTimeout(() => setQuery(isLink ? "" : text.trim()), 300);
    return () => clearTimeout(t);
  }, [text, isLink]);

  const results = useQuery({
    queryKey: ["radar", path.workspaceId, { q: query, per_page: 6 }],
    enabled: query.length >= 2,
    queryFn: () =>
      unwrap(
        api.GET("/v1/workspaces/{workspaceId}/radar", {
          params: { path: { workspaceId: path.workspaceId }, query: { q: query, per_page: 6 } },
        }),
      ),
  });
  const add = useMutation({
    mutationFn: (body: { product_id: string } | { url: string }) =>
      unwrap(api.POST("/v1/workspaces/{workspaceId}/lists/{listId}/items", { params: { path }, body })),
    onSuccess: (l) => {
      update(l);
      setText("");
    },
  });

  return (
    <Card className="flex flex-col gap-3 p-4">
      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          if (isLink) add.mutate({ url: text.trim() });
        }}
      >
        <div className="relative flex-1">
          <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted" />
          <Input
            aria-label="Adicionar produto"
            className="pl-9"
            placeholder="Busque no radar ou cole o link de um produto da Shopee"
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              add.reset();
            }}
          />
        </div>
        {isLink && (
          <Button type="submit" disabled={add.isPending}>
            <Plus className="size-4" /> {add.isPending ? "Adicionando…" : "Adicionar"}
          </Button>
        )}
      </form>
      {add.error && <p className="text-sm text-red-700">{add.error.message}</p>}
      {query.length >= 2 && results.data && (
        <ul className="flex flex-col divide-y divide-border">
          {results.data.items.length === 0 && (
            <li className="py-2 text-sm text-muted">Nada no radar com esse nome. Cole o link do produto.</li>
          )}
          {results.data.items.map((r) => (
            <li key={r.product_id} className="flex items-center gap-3 py-2">
              <ProductImage src={r.image_url} className="size-10 shrink-0 rounded" />
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm">{r.name}</p>
                <p className="text-xs text-muted">
                  {money(r.min_price_cents)} · comissão {percent(r.commission_bp)}
                </p>
              </div>
              <Button
                size="sm"
                variant="secondary"
                disabled={add.isPending}
                onClick={() => add.mutate({ product_id: r.product_id })}
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
