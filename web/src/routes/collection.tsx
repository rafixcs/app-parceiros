import { keepPreviousData, useMutation, useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ChevronLeft, ChevronRight, Copy, Link2, Plus, Search } from "lucide-react";
import { useEffect, useState } from "react";
import { api, unwrap, type Collection, type Item } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Notice, Badge, Card } from "@/components/ui/card";
import { Input, Select } from "@/components/ui/input";
import { type Channel, channels, copyText, textToCopy } from "@/lib/clipboard";
import { money, priceRange } from "@/lib/format";
import { cn } from "@/lib/utils";
import { useCollections, useInvalidateCollection, useSaveItem } from "./collection-api";
import { useShopeeConnection } from "./layout";
import { ProductImage } from "./radar";
import { collectionRoute } from "./router";

export const itemStatuses = {
  testing: { label: "Testando", className: "bg-sky-50 text-sky-800" },
  winner: { label: "Campeão", className: "bg-amber-50 text-amber-800" },
  discarded: { label: "Descartado", className: "bg-zinc-100 text-zinc-600" },
} as const satisfies Record<Item["status"], { label: string; className: string }>;
type ItemStatus = keyof typeof itemStatuses;

export type CollectionSearch = {
  q?: string;
  status?: ItemStatus;
  collection?: string;
  tag?: string;
  page?: number;
};

const PER_PAGE = 30;

export function validateCollectionSearch(s: Record<string, unknown>): CollectionSearch {
  const text = (v: unknown, max: number) => (typeof v === "string" && v ? v.slice(0, max) : undefined);
  const page = Number(s.page);
  return {
    q: text(s.q, 100),
    status: typeof s.status === "string" && s.status in itemStatuses ? (s.status as ItemStatus) : undefined,
    collection: text(s.collection, 36),
    tag: text(s.tag, 30),
    page: Number.isInteger(page) && page > 1 ? page : undefined,
  };
}

const channelKey = "parceiros.channel";

/** Channel picked for the quick copy, remembered between visits. */
export function useChannel(): [Channel, (c: Channel) => void] {
  const [channel, setChannel] = useState<Channel>(() => {
    try {
      const c = localStorage.getItem(channelKey);
      return c && c in channels ? (c as Channel) : "instagram";
    } catch {
      return "instagram";
    }
  });
  return [
    channel,
    (c) => {
      setChannel(c);
      try {
        localStorage.setItem(channelKey, c);
      } catch {
        // Without localStorage, it just does not remember the choice.
      }
    },
  ];
}

/** Repeats the query while some link is being generated. */
export function generating(items: Pick<Item, "link_status">[] | undefined) {
  return items?.some((i) => i.link_status === "generating") ? 2500 : false;
}

export function CollectionPage() {
  const { workspaceId } = collectionRoute.useParams();
  const search = collectionRoute.useSearch();
  const navigate = useNavigate({ from: collectionRoute.fullPath });
  const page = search.page ?? 1;
  const [channel, setChannel] = useChannel();

  const items = useQuery({
    queryKey: ["items", workspaceId, search],
    queryFn: () =>
      unwrap(
        api.GET("/v1/workspaces/{workspaceId}/items", {
          params: { path: { workspaceId }, query: { ...search, page, per_page: PER_PAGE } },
        }),
      ),
    placeholderData: keepPreviousData,
    refetchInterval: (q) => generating(q.state.data?.items),
  });
  const collections = useCollections(workspaceId);
  const connection = useShopeeConnection();
  const invalidate = useInvalidateCollection(workspaceId);
  const pending = useMutation({
    mutationFn: () =>
      unwrap(api.POST("/v1/workspaces/{workspaceId}/items/pending-links", { params: { path: { workspaceId } } })),
    onSuccess: () => invalidate(),
  });

  const filter = (m: Partial<CollectionSearch>) => navigate({ search: (a) => ({ ...a, ...m, page: undefined }) });
  const total = items.data?.total ?? 0;
  const pages = Math.max(1, Math.ceil(total / PER_PAGE));
  const connected = connection.data?.status === "connected";
  const missingLink = items.data?.items.some((i) => i.link_status === "pending" || i.link_status === "failed");
  const empty = items.data && total === 0 && !search.q && !search.status && !search.collection && !search.tag;

  return (
    <div className="flex flex-col gap-5">
      <div>
        <h1 className="text-2xl font-semibold">Minha coleção</h1>
        <p className="text-sm text-muted">
          Os produtos que você vai divulgar, com o seu link de afiliado. Só você vê esta coleção.
        </p>
      </div>

      {connection.data && !connected && (
        <Notice className="border-amber-200 bg-amber-50 text-amber-900">
          Os seus links ficam pendentes até você{" "}
          <Link to="/conta/shopee" className="font-medium underline">
            conectar a sua conta de afiliado da Shopee
          </Link>
          .
        </Notice>
      )}
      {connected && missingLink && (
        <Notice className="flex flex-wrap items-center gap-3 border-amber-200 bg-amber-50 text-amber-900">
          <span className="flex-1">Alguns produtos ainda estão sem link de afiliado.</span>
          <Button size="sm" disabled={pending.isPending} onClick={() => pending.mutate()}>
            Gerar links pendentes
          </Button>
        </Notice>
      )}

      <PasteLink workspaceId={workspaceId} />

      <CollectionBar
        workspaceId={workspaceId}
        current={search.collection}
        collections={collections.data ?? []}
        onPick={(collection) => filter({ collection })}
      />

      <Filters search={search} onFilter={filter} channel={channel} onChannel={setChannel} />

      {items.error && <Notice className="border-red-200 bg-red-50 text-red-800">{items.error.message}</Notice>}
      {items.isPending && <p className="text-sm text-muted">Carregando…</p>}
      {empty && (
        <Card className="p-8 text-center text-sm text-muted">
          Você ainda não salvou nenhum produto. Salve pelo{" "}
          <Link to="/w/$workspaceId/radar" params={{ workspaceId }} search={{}} className="text-brand hover:underline">
            radar
          </Link>{" "}
          ou cole o link de um produto da Shopee acima.
        </Card>
      )}
      {items.data && total === 0 && !empty && (
        <Card className="p-8 text-center text-sm text-muted">Nenhum produto com esses filtros.</Card>
      )}

      <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {items.data?.items.map((it) => (
          <li key={it.id}>
            <ItemCard item={it} workspaceId={workspaceId} channel={channel} onTag={(tag) => filter({ tag })} />
          </li>
        ))}
      </ul>

      {total > PER_PAGE && (
        <nav className="flex items-center justify-center gap-3 text-sm" aria-label="Páginas">
          <Button
            variant="secondary"
            size="sm"
            disabled={page <= 1}
            onClick={() => navigate({ search: (a) => ({ ...a, page: page - 1 }) })}
          >
            <ChevronLeft className="size-4" /> Anterior
          </Button>
          <span className="text-muted">
            Página {page} de {pages}
          </span>
          <Button
            variant="secondary"
            size="sm"
            disabled={page >= pages}
            onClick={() => navigate({ search: (a) => ({ ...a, page: page + 1 }) })}
          >
            Próxima <ChevronRight className="size-4" />
          </Button>
        </nav>
      )}
    </div>
  );
}

function PasteLink({ workspaceId }: { workspaceId: string }) {
  const navigate = useNavigate();
  const [url, setUrl] = useState("");
  const save = useSaveItem(workspaceId);
  return (
    <Card className="p-3">
      <form
        className="flex flex-col gap-2 sm:flex-row"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate(
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
          <Link2 className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted" />
          <Input
            aria-label="Link do produto na Shopee"
            className="pl-9"
            placeholder="Cole o link de um produto da Shopee"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            required
          />
        </div>
        <Button type="submit" disabled={save.isPending || !url.trim()}>
          {save.isPending ? "Buscando…" : "Salvar produto"}
        </Button>
      </form>
      {save.error && <p className="mt-2 text-sm text-red-700">{save.error.message}</p>}
    </Card>
  );
}

function CollectionBar({
  workspaceId,
  current,
  collections,
  onPick,
}: {
  workspaceId: string;
  current?: string;
  collections: Collection[];
  onPick: (id?: string) => void;
}) {
  const [draft, setDraft] = useState<string | null>(null);
  const invalidate = useInvalidateCollection(workspaceId);
  const create = useMutation({
    mutationFn: (name: string) =>
      unwrap(api.POST("/v1/workspaces/{workspaceId}/collections", { params: { path: { workspaceId } }, body: { name } })),
    onSuccess: () => {
      setDraft(null);
      invalidate();
    },
  });
  const chip = (active: boolean) =>
    cn(
      "h-8 rounded-full border px-3 text-sm",
      active ? "border-brand bg-brand text-white" : "border-border bg-white hover:bg-zinc-50",
    );

  return (
    <div className="flex flex-wrap items-center gap-2">
      <button type="button" className={chip(!current)} onClick={() => onPick(undefined)}>
        Todos
      </button>
      {collections.map((c) => (
        <button key={c.id} type="button" className={chip(current === c.id)} onClick={() => onPick(c.id)}>
          {c.name} <span className="opacity-70">{c.items}</span>
        </button>
      ))}
      {draft === null ? (
        <Button variant="ghost" size="sm" onClick={() => setDraft("")}>
          <Plus className="size-4" /> Nova coleção
        </Button>
      ) : (
        <form
          className="flex items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate(draft);
          }}
        >
          <Input
            autoFocus
            aria-label="Nome da coleção"
            className="h-8 w-48"
            maxLength={60}
            placeholder="Ex.: Achados da semana"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => e.key === "Escape" && setDraft(null)}
          />
          <Button size="sm" type="submit" disabled={create.isPending || !draft.trim()}>
            Criar
          </Button>
          {create.error && <span className="text-sm text-red-700">{create.error.message}</span>}
        </form>
      )}
    </div>
  );
}

function Filters({
  search,
  onFilter,
  channel,
  onChannel,
}: {
  search: CollectionSearch;
  onFilter: (m: Partial<CollectionSearch>) => void;
  channel: Channel;
  onChannel: (c: Channel) => void;
}) {
  const [q, setQ] = useState(search.q ?? "");
  useEffect(() => {
    const t = setTimeout(() => {
      if ((search.q ?? "") !== q.trim()) onFilter({ q: q.trim() || undefined });
    }, 350);
    return () => clearTimeout(t);
  }, [q]); // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
      <div className="relative col-span-2">
        <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted" />
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
        value={search.status ?? ""}
        onChange={(e) => onFilter({ status: (e.target.value || undefined) as ItemStatus | undefined })}
      >
        <option value="">Todos os status</option>
        {Object.entries(itemStatuses).map(([v, s]) => (
          <option key={v} value={v}>
            {s.label}
          </option>
        ))}
      </Select>
      <Select aria-label="Canal do link ao copiar" value={channel} onChange={(e) => onChannel(e.target.value as Channel)}>
        {Object.entries(channels).map(([v, label]) => (
          <option key={v} value={v}>
            Copiar para {label}
          </option>
        ))}
      </Select>
      {search.tag && (
        <div className="col-span-2 flex items-center gap-2 text-sm md:col-span-4">
          Tag: <Badge className="bg-zinc-100 text-zinc-700">{search.tag}</Badge>
          <button type="button" className="text-muted hover:underline" onClick={() => onFilter({ tag: undefined })}>
            limpar
          </button>
        </div>
      )}
    </div>
  );
}

export function LinkStatus({ item }: { item: Pick<Item, "link_status" | "link_origin"> }) {
  switch (item.link_status) {
    case "ready":
      return item.link_origin === "manual" ? (
        <Badge className="bg-emerald-50 text-emerald-800">Link manual</Badge>
      ) : (
        <Badge className="bg-emerald-50 text-emerald-800">Link pronto</Badge>
      );
    case "generating":
      return <Badge className="bg-sky-50 text-sky-800">Gerando link…</Badge>;
    case "pending":
      return (
        <Badge className="bg-amber-50 text-amber-800" title="Conecte a sua conta da Shopee para gerar o link">
          Link pendente
        </Badge>
      );
    case "failed":
      return <Badge className="bg-red-50 text-red-700">Link falhou</Badge>;
  }
}

function ItemCard({
  item: it,
  workspaceId,
  channel,
  onTag,
}: {
  item: Item;
  workspaceId: string;
  channel: Channel;
  onTag: (t: string) => void;
}) {
  const [copied, setCopied] = useState<boolean | null>(null);
  const s = itemStatuses[it.status];
  return (
    <Card className="flex h-full flex-col gap-2 p-3">
      <Link
        to="/w/$workspaceId/colecao/$itemId"
        params={{ workspaceId, itemId: it.id }}
        className="flex gap-3 hover:opacity-90"
      >
        <ProductImage src={it.product.image_url} className="size-20 shrink-0 rounded-lg" />
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <p className="line-clamp-2 text-sm font-medium leading-snug">{it.title || it.product.name}</p>
          <p className="text-sm">{priceRange(it.product.min_price_cents, it.product.max_price_cents)}</p>
          <p className="text-xs text-emerald-700">Ganha {money(it.product.earnings_per_sale_cents)} por venda</p>
        </div>
      </Link>
      <div className="flex flex-wrap items-center gap-1.5">
        <Badge className={s.className}>{s.label}</Badge>
        <LinkStatus item={it} />
        {it.tags.map((t) => (
          <button key={t} type="button" onClick={() => onTag(t)}>
            <Badge className="bg-zinc-100 text-zinc-700 hover:bg-zinc-200">#{t}</Badge>
          </button>
        ))}
      </div>
      <Button
        size="sm"
        variant="secondary"
        className="mt-auto"
        onClick={async () => setCopied(await copyText(textToCopy(it, channel)))}
        onBlur={() => setCopied(null)}
      >
        <Copy className="size-4" />
        {copied === true ? "Copiado!" : copied === false ? "Não deu para copiar" : `Copiar para ${channels[channel]}`}
      </Button>
    </Card>
  );
}
