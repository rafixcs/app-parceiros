import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { ChevronLeft, ChevronRight, Flame, Search, Star } from "lucide-react";
import { useEffect, useState } from "react";
import { api, unwrap, type Schemas, type Trend } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Badge, Card, Notice } from "@/components/ui/card";
import { Field, Input, Select } from "@/components/ui/input";
import { count, money, percent, priceRange, timeAgo, toCents } from "@/lib/format";
import { radarRoute } from "./router";
import { SaveButton } from "./save";

const sorts = {
  trend: "Em alta",
  earnings: "Maior ganho por venda",
  commission: "Maior comissão",
  sales: "Mais vendidos",
} as const satisfies Record<Schemas["RadarSort"], string>;
type Sort = keyof typeof sorts;

export type RadarSearch = {
  q?: string;
  category?: number;
  min_price?: number;
  max_price?: number;
  min_commission?: number;
  min_rating?: number;
  sort?: Sort;
  page?: number;
};

const PER_PAGE = 24;

function number(v: unknown): number | undefined {
  const n = typeof v === "number" ? v : typeof v === "string" && v !== "" ? Number(v) : NaN;
  return Number.isFinite(n) && n >= 0 ? n : undefined;
}

/** The filters live in the URL, so a link to the filtered radar can be shared. */
export function validateRadarSearch(s: Record<string, unknown>): RadarSearch {
  const sort = typeof s.sort === "string" && s.sort in sorts ? (s.sort as Sort) : undefined;
  const page = number(s.page);
  return {
    q: typeof s.q === "string" && s.q ? s.q.slice(0, 100) : undefined,
    category: number(s.category),
    min_price: number(s.min_price),
    max_price: number(s.max_price),
    min_commission: number(s.min_commission),
    min_rating: number(s.min_rating),
    sort,
    page: page && page > 1 ? Math.floor(page) : undefined,
  };
}

export function Radar() {
  const { workspaceId } = radarRoute.useParams();
  const search = radarRoute.useSearch();
  const navigate = useNavigate({ from: radarRoute.fullPath });
  const page = search.page ?? 1;

  const radar = useQuery({
    queryKey: ["radar", workspaceId, search],
    queryFn: () =>
      unwrap(
        api.GET("/v1/workspaces/{workspaceId}/radar", {
          params: { path: { workspaceId }, query: { ...search, page, per_page: PER_PAGE } },
        }),
      ),
    placeholderData: keepPreviousData,
  });
  const categories = useQuery({
    queryKey: ["radar-categories", workspaceId],
    queryFn: () =>
      unwrap(api.GET("/v1/workspaces/{workspaceId}/radar/categories", { params: { path: { workspaceId } } })),
    staleTime: 10 * 60_000,
  });

  const filter = (change: Partial<RadarSearch>) =>
    navigate({ search: (current) => ({ ...current, ...change, page: undefined }) });

  const total = radar.data?.total ?? 0;
  const pages = Math.max(1, Math.ceil(total / PER_PAGE));

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <h1 className="text-2xl font-semibold">Radar</h1>
          <p className="text-sm text-muted">
            Produtos da Shopee em alta, com quanto você ganha por venda.
            {radar.data?.updated_at && <> Atualizado {timeAgo(radar.data.updated_at)}.</>}
          </p>
        </div>
      </div>

      <Filters search={search} categories={categories.data ?? []} onFilter={filter} />

      {radar.error && (
        <Notice className="border-red-200 bg-red-50 text-red-800">{radar.error.message}</Notice>
      )}
      {radar.isPending && <p className="text-sm text-muted">Carregando produtos…</p>}
      {radar.data && radar.data.items.length === 0 && (
        <Card className="p-8 text-center text-sm text-muted">
          {radar.data.updated_at
            ? "Nenhum produto com esses filtros. Tente afrouxar algum."
            : "O radar ainda não tem produtos. A primeira coleta da Shopee roda logo depois que o worker sobe."}
        </Card>
      )}

      <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {radar.data?.items.map((p) => (
          <li key={p.product_id} className="relative">
            <ProductCard product={p} workspaceId={workspaceId} />
            <SaveButton workspaceId={workspaceId} productId={p.product_id} className="absolute right-3 bottom-3" />
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
            Página {page} de {pages} · {count(total)} produtos
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

function Filters({
  search,
  categories,
  onFilter,
}: {
  search: RadarSearch;
  categories: { id: number; name: string }[];
  onFilter: (m: Partial<RadarSearch>) => void;
}) {
  const [q, setQ] = useState(search.q ?? "");
  const [minPrice, setMinPrice] = useState(search.min_price ? String(search.min_price / 100) : "");
  const [maxPrice, setMaxPrice] = useState(search.max_price ? String(search.max_price / 100) : "");

  // The text search filters while the person types, after a pause.
  useEffect(() => {
    const t = setTimeout(() => {
      if ((search.q ?? "") !== q.trim()) onFilter({ q: q.trim() || undefined });
    }, 350);
    return () => clearTimeout(t);
  }, [q]); // eslint-disable-line react-hooks/exhaustive-deps

  const applyPrice = () => {
    const min = toCents(minPrice);
    const max = toCents(maxPrice);
    if (min !== search.min_price || max !== search.max_price) onFilter({ min_price: min, max_price: max });
  };
  const applyOnEnter = (e: React.KeyboardEvent) => e.key === "Enter" && applyPrice();

  return (
    <Card className="grid grid-cols-2 gap-3 p-3 md:grid-cols-6">
      <div className="relative col-span-2">
        <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted" />
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
        value={search.category ?? ""}
        onChange={(e) => onFilter({ category: e.target.value ? Number(e.target.value) : undefined })}
      >
        <option value="">Todas as categorias</option>
        {categories.map((c) => (
          <option key={c.id} value={c.id}>
            {c.name}
          </option>
        ))}
      </Select>
      <Select
        aria-label="Ordenar por"
        value={search.sort ?? "trend"}
        onChange={(e) => onFilter({ sort: e.target.value === "trend" ? undefined : (e.target.value as Sort) })}
      >
        {Object.entries(sorts).map(([v, label]) => (
          <option key={v} value={v}>
            {label}
          </option>
        ))}
      </Select>
      <Select
        aria-label="Comissão mínima"
        value={search.min_commission ?? ""}
        onChange={(e) => onFilter({ min_commission: e.target.value ? Number(e.target.value) : undefined })}
      >
        <option value="">Qualquer comissão</option>
        {[500, 1000, 1500, 2000].map((bp) => (
          <option key={bp} value={bp}>
            A partir de {percent(bp)}
          </option>
        ))}
      </Select>
      <Select
        aria-label="Nota mínima"
        value={search.min_rating ?? ""}
        onChange={(e) => onFilter({ min_rating: e.target.value ? Number(e.target.value) : undefined })}
      >
        <option value="">Qualquer nota</option>
        {[4, 4.5, 4.8].map((n) => (
          <option key={n} value={n}>
            {n.toLocaleString("pt-BR")} estrelas ou mais
          </option>
        ))}
      </Select>
      <div className="col-span-2 flex items-end gap-2 md:col-span-6">
        <Field label="Preço de (R$)">
          <Input inputMode="decimal" value={minPrice} onChange={(e) => setMinPrice(e.target.value)} onBlur={applyPrice} onKeyDown={applyOnEnter} />
        </Field>
        <Field label="até (R$)">
          <Input inputMode="decimal" value={maxPrice} onChange={(e) => setMaxPrice(e.target.value)} onBlur={applyPrice} onKeyDown={applyOnEnter} />
        </Field>
      </div>
    </Card>
  );
}

function ProductCard({ product: p, workspaceId }: { product: Trend; workspaceId: string }) {
  return (
    <Link
      to="/w/$workspaceId/radar/$productId"
      params={{ workspaceId, productId: p.product_id }}
      className="group block h-full"
    >
      <Card className="flex h-full gap-3 p-3 transition-shadow group-hover:shadow-md">
        <ProductImage src={p.image_url} className="size-24 shrink-0 rounded-lg" />
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <p className="line-clamp-2 text-sm font-medium leading-snug">{p.name}</p>
          <p className="truncate text-xs text-muted">{p.shop_name}</p>
          <p className="text-sm">{priceRange(p.min_price_cents, p.max_price_cents)}</p>
          <div className="mt-auto flex flex-wrap items-center gap-1.5">
            <Badge className="bg-emerald-50 text-emerald-800" title="Comissão × preço mínimo">
              Ganha {money(p.earnings_per_sale_cents)}
            </Badge>
            <Badge className="bg-zinc-100 text-zinc-700">{percent(p.commission_bp)}</Badge>
            <TrendBadge product={p} />
          </div>
          {/* Room for the Salvar button, which sits outside the card's link. */}
          <p className="flex min-h-8 items-center gap-2 pr-24 text-xs text-muted">
            {count(p.sales)} vendidos
            {p.rating != null && (
              <span className="flex items-center gap-0.5">
                <Star className="size-3 fill-amber-400 text-amber-400" />
                {p.rating.toLocaleString("pt-BR")}
              </span>
            )}
          </p>
        </div>
      </Card>
    </Link>
  );
}

export function TrendBadge({ product: p }: { product: Trend }) {
  if (p.sales_growth_7d == null) {
    return (
      <Badge className="bg-zinc-100 text-muted" title="Ainda não há um dia de histórico para medir o crescimento">
        Sem histórico
      </Badge>
    );
  }
  return (
    <Badge className="bg-orange-50 text-brand-dark" title={`+${count(p.sales_growth_7d)} vendas em 7 dias`}>
      <Flame className="size-3" />
      {Math.round(p.score)}
    </Badge>
  );
}

export function ProductImage({ src, className }: { src: string | null; className?: string }) {
  const [failed, setFailed] = useState(false);
  if (!src || failed) return <div className={`bg-zinc-100 ${className ?? ""}`} aria-hidden />;
  return (
    <img
      src={src}
      alt=""
      loading="lazy"
      referrerPolicy="no-referrer"
      onError={() => setFailed(true)}
      className={`object-cover ${className ?? ""}`}
    />
  );
}
