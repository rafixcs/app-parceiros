import { useMutation, useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ArrowLeft, Copy, ExternalLink, RefreshCw, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { api, unwrap, type Item, type Schemas } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Notice, Card } from "@/components/ui/card";
import { Input, Field, Select, Textarea } from "@/components/ui/input";
import { type Channel, channels, channelLink, copyText, textToCopy } from "@/lib/clipboard";
import { money, percent, priceRange } from "@/lib/format";
import { generating, itemStatuses, LinkStatus, useChannel } from "./collection";
import { useCollections, useInvalidateCollection } from "./collection-api";
import { ProductVideos } from "./product-videos";
import { ProductImage } from "./radar";
import { itemRoute } from "./router";

type ItemUpdate = Schemas["UpdateItem"];

export function ItemPage() {
  const { workspaceId, itemId } = itemRoute.useParams();
  const navigate = useNavigate();
  const invalidate = useInvalidateCollection(workspaceId);
  const path = { params: { path: { workspaceId, itemId } } };

  const item = useQuery({
    queryKey: ["item", workspaceId, itemId],
    queryFn: () => unwrap(api.GET("/v1/workspaces/{workspaceId}/items/{itemId}", path)),
    refetchInterval: (q) => generating(q.state.data ? [q.state.data] : undefined),
  });
  const update = useMutation({
    mutationFn: (body: ItemUpdate) => unwrap(api.PATCH("/v1/workspaces/{workspaceId}/items/{itemId}", { ...path, body })),
    onSuccess: invalidate,
  });
  const remove = useMutation({
    mutationFn: () => unwrap(api.DELETE("/v1/workspaces/{workspaceId}/items/{itemId}", path)),
    onSuccess: async () => {
      invalidate();
      await navigate({ to: "/w/$workspaceId/colecao", params: { workspaceId }, search: {} });
    },
  });

  const back = (
    <Link
      to="/w/$workspaceId/colecao"
      params={{ workspaceId }}
      search={{}}
      className="inline-flex items-center gap-1 text-sm text-muted hover:text-foreground"
    >
      <ArrowLeft className="size-4" /> Minha coleção
    </Link>
  );
  if (item.error) {
    return (
      <div className="flex flex-col gap-4">
        {back}
        <Notice className="border-red-200 bg-red-50 text-red-800">{item.error.message}</Notice>
      </div>
    );
  }
  if (item.isPending) return <p className="text-sm text-muted">Carregando…</p>;
  const it = item.data;
  const p = it.product;

  return (
    <div className="flex flex-col gap-4">
      {back}
      <Card className="flex gap-4 p-4">
        <ProductImage src={p.image_url} className="size-24 shrink-0 rounded-lg sm:size-32" />
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <p className="text-sm text-muted">{p.shop_name}</p>
          <p className="font-medium leading-snug">{p.name}</p>
          <p className="text-sm">
            {priceRange(p.min_price_cents, p.max_price_cents)} · comissão {percent(p.commission_bp)} ·{" "}
            <span className="font-medium text-emerald-700">ganha {money(p.earnings_per_sale_cents)} por venda</span>
          </p>
          <a
            href={p.url}
            target="_blank"
            rel="noreferrer"
            className="mt-auto inline-flex items-center gap-1 text-sm text-brand hover:underline"
          >
            Ver na Shopee <ExternalLink className="size-3.5" />
          </a>
        </div>
      </Card>

      <div className="grid gap-4 lg:grid-cols-[1fr_22rem]">
        <div className="flex min-w-0 flex-col gap-4">
          <ItemForm item={it} saving={update.isPending} error={update.error?.message} onSave={(b) => update.mutate(b)} />
          <ProductVideos workspaceId={workspaceId} productId={p.id} />
        </div>
        <div className="flex flex-col gap-4">
          <QuickCopy item={it} />
          <AffiliateLink item={it} workspaceId={workspaceId} />
          <ItemCollections item={it} workspaceId={workspaceId} />
          <Button
            variant="danger"
            disabled={remove.isPending}
            onClick={() => {
              if (confirm("Remover este produto da sua coleção? As notas e os links dele serão apagados.")) remove.mutate();
            }}
          >
            <Trash2 className="size-4" /> Remover da coleção
          </Button>
        </div>
      </div>
    </div>
  );
}

function ItemForm({
  item,
  saving,
  error,
  onSave,
}: {
  item: Item;
  saving: boolean;
  error?: string;
  onSave: (b: ItemUpdate) => void;
}) {
  const [title, setTitle] = useState(item.title);
  const [description, setDescription] = useState(item.description);
  const [notes, setNotes] = useState(item.notes);
  const [tags, setTags] = useState(item.tags.join(", "));
  const [status, setStatus] = useState(item.status);
  // Reloads the form when another item opens.
  useEffect(() => {
    setTitle(item.title);
    setDescription(item.description);
    setNotes(item.notes);
    setTags(item.tags.join(", "));
    setStatus(item.status);
  }, [item.id]); // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <Card className="p-4">
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          onSave({
            title,
            description,
            notes,
            status,
            tags: tags
              .split(",")
              .map((t) => t.trim())
              .filter(Boolean),
          });
        }}
      >
        <Field label="Título para divulgar">
          <Input value={title} maxLength={200} onChange={(e) => setTitle(e.target.value)} placeholder={item.product.name} />
        </Field>
        <Field label="Descrição">
          <Textarea
            rows={4}
            maxLength={2000}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="O texto que vai junto do link nas redes sociais"
          />
        </Field>
        <Field label="Notas (só você vê)">
          <Textarea rows={3} maxLength={5000} value={notes} onChange={(e) => setNotes(e.target.value)} />
        </Field>
        <div className="grid grid-cols-2 gap-3">
          <Field label="Tags, separadas por vírgula">
            <Input value={tags} onChange={(e) => setTags(e.target.value)} placeholder="casa, presente" />
          </Field>
          <Field label="Status">
            <Select value={status} onChange={(e) => setStatus(e.target.value as Item["status"])}>
              {Object.entries(itemStatuses).map(([v, s]) => (
                <option key={v} value={v}>
                  {s.label}
                </option>
              ))}
            </Select>
          </Field>
        </div>
        {error && <Notice className="border-red-200 bg-red-50 text-red-800">{error}</Notice>}
        <Button type="submit" disabled={saving} className="self-start">
          {saving ? "Salvando…" : "Salvar alterações"}
        </Button>
      </form>
    </Card>
  );
}

function QuickCopy({ item }: { item: Item }) {
  const [channel, setChannel] = useChannel();
  const [copied, setCopied] = useState<boolean | null>(null);
  const text = textToCopy(item, channel);
  return (
    <Card className="flex flex-col gap-3 p-4">
      <h2 className="font-medium">Copiar rápido</h2>
      <Select aria-label="Canal" value={channel} onChange={(e) => setChannel(e.target.value as Channel)}>
        {Object.entries(channels).map(([v, label]) => (
          <option key={v} value={v}>
            {label}
          </option>
        ))}
      </Select>
      <pre className="max-h-40 overflow-auto rounded-lg bg-zinc-50 p-3 text-xs whitespace-pre-wrap">{text}</pre>
      {!channelLink(item, channel) && (
        <p className="text-xs text-amber-800">O link ainda não está pronto; o texto vai sem ele.</p>
      )}
      <Button onClick={async () => setCopied(await copyText(text))} onBlur={() => setCopied(null)}>
        <Copy className="size-4" />
        {copied === true ? "Copiado!" : copied === false ? "Não deu para copiar" : "Copiar título, descrição e link"}
      </Button>
    </Card>
  );
}

function AffiliateLink({ item, workspaceId }: { item: Item; workspaceId: string }) {
  const invalidate = useInvalidateCollection(workspaceId);
  const path = { params: { path: { workspaceId, itemId: item.id } } };
  const [manual, setManual] = useState("");
  const setLink = useMutation({
    mutationFn: (link: string | null) =>
      unwrap(api.PATCH("/v1/workspaces/{workspaceId}/items/{itemId}", { ...path, body: { affiliate_link: link } })),
    onSuccess: (it) => {
      setManual("");
      invalidate(it);
    },
  });
  const regenerate = useMutation({
    mutationFn: () => unwrap(api.POST("/v1/workspaces/{workspaceId}/items/{itemId}/link", path)),
    onSuccess: invalidate,
  });

  return (
    <Card className="flex flex-col gap-3 p-4">
      <div className="flex items-center gap-2">
        <h2 className="mr-auto font-medium">Link de afiliado</h2>
        <LinkStatus item={item} />
      </div>
      {item.link_status === "pending" && (
        <p className="text-sm text-muted">
          <Link to="/conta/shopee" className="text-brand hover:underline">
            Conecte a sua conta da Shopee
          </Link>{" "}
          para gerar o link com o seu ID de afiliado.
        </p>
      )}
      {item.link_status === "failed" && (
        <p className="text-sm text-muted">A Shopee não gerou o link. Tente de novo em instantes.</p>
      )}
      {item.link_origin === "manual" && item.affiliate_link && (
        <p className="truncate text-sm">
          Usando o seu link: <span className="font-mono text-xs">{item.affiliate_link}</span>
        </p>
      )}
      {item.links.length > 0 && (
        <ul className="flex flex-col gap-1.5">
          {item.links.map((l) => (
            <li key={l.channel} className="flex items-center gap-2 text-sm">
              <span className="w-20 shrink-0 text-muted">{channels[l.channel]}</span>
              <span className="min-w-0 flex-1 truncate font-mono text-xs" title={`subId ${l.sub_id}`}>
                {l.url}
              </span>
              <Button
                variant="ghost"
                size="sm"
                aria-label={`Copiar link do ${channels[l.channel]}`}
                onClick={() => copyText(l.url)}
              >
                <Copy className="size-4" />
              </Button>
            </li>
          ))}
        </ul>
      )}
      <Button
        variant="secondary"
        size="sm"
        disabled={regenerate.isPending || item.link_status === "generating"}
        onClick={() => regenerate.mutate()}
      >
        <RefreshCw className="size-4" />
        {item.link_origin === "manual" ? "Voltar ao link automático" : "Gerar de novo"}
      </Button>
      <form
        className="flex flex-col gap-2 border-t border-border pt-3"
        onSubmit={(e) => {
          e.preventDefault();
          setLink.mutate(manual.trim());
        }}
      >
        <Field label="Ou use um link seu">
          <Input
            type="url"
            placeholder="https://s.shopee.com.br/..."
            value={manual}
            onChange={(e) => setManual(e.target.value)}
            required
          />
        </Field>
        {setLink.error && <p className="text-sm text-red-700">{setLink.error.message}</p>}
        <Button type="submit" size="sm" variant="secondary" disabled={setLink.isPending || !manual.trim()}>
          Usar este link
        </Button>
      </form>
    </Card>
  );
}

function ItemCollections({ item, workspaceId }: { item: Item; workspaceId: string }) {
  const collections = useCollections(workspaceId);
  const invalidate = useInvalidateCollection(workspaceId);
  const setCollections = useMutation({
    mutationFn: (ids: string[]) =>
      unwrap(
        api.PUT("/v1/workspaces/{workspaceId}/items/{itemId}/collections", {
          params: { path: { workspaceId, itemId: item.id } },
          body: { collection_ids: ids },
        }),
      ),
    onSuccess: invalidate,
  });
  const checked = new Set(item.collection_ids);

  return (
    <Card className="flex flex-col gap-2 p-4">
      <h2 className="font-medium">Coleções</h2>
      {collections.data?.length === 0 && (
        <p className="text-sm text-muted">Crie coleções na página da sua coleção para organizar os produtos.</p>
      )}
      {collections.data?.map((c) => (
        <label key={c.id} className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            className="size-4 accent-brand"
            checked={checked.has(c.id)}
            disabled={setCollections.isPending}
            onChange={(e) => {
              const ids = new Set(checked);
              if (e.target.checked) ids.add(c.id);
              else ids.delete(c.id);
              setCollections.mutate([...ids]);
            }}
          />
          {c.name}
        </label>
      ))}
      {setCollections.error && <p className="text-sm text-red-700">{setCollections.error.message}</p>}
    </Card>
  );
}
