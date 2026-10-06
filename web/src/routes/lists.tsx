import { Link, useNavigate } from "@tanstack/react-router";
import { ListChecks, Plus } from "lucide-react";
import { useState } from "react";
import type { CuratedList } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Notice, Badge, Card } from "@/components/ui/card";
import { Input, Field, Textarea } from "@/components/ui/input";
import { timeAgo } from "@/lib/format";
import { useCurrentWorkspace } from "./layout";
import { useCreateList, useLists } from "./lists-api";
import { listsRoute } from "./router";

export function Lists() {
  const { workspaceId } = listsRoute.useParams();
  const { workspace, manager } = useCurrentWorkspace(workspaceId);
  const lists = useLists(workspaceId);
  const [creating, setCreating] = useState(false);

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <h1 className="text-2xl font-semibold">Listas</h1>
          <p className="text-sm text-muted">
            {manager
              ? "Monte listas de produtos com as suas dicas e publique para a turma."
              : "Produtos escolhidos pelo seu mentor. Importe para a sua coleção e receba o seu link."}
          </p>
        </div>
        {manager && !creating && (
          <Button onClick={() => setCreating(true)}>
            <Plus className="size-4" /> Nova lista
          </Button>
        )}
      </div>

      {manager && creating && <NewList workspaceId={workspaceId} onCancel={() => setCreating(false)} />}

      {workspace?.kind === "personal" && (
        <Notice className="border-border bg-white text-muted">
          As listas da curadoria ficam nos workspaces de mentoria. Escolha uma mentoria no seletor do topo, ou{" "}
          <Link to="/w/$workspaceId/turma" params={{ workspaceId }} className="text-brand hover:underline">
            crie a sua
          </Link>
          .
        </Notice>
      )}
      {lists.error && <Notice className="border-red-200 bg-red-50 text-red-800">{lists.error.message}</Notice>}
      {lists.isPending && <p className="text-sm text-muted">Carregando listas…</p>}
      {lists.data?.length === 0 && workspace?.kind === "mentorship" && (
        <Card className="p-8 text-center text-sm text-muted">
          {manager
            ? "Nenhuma lista ainda. Crie a primeira com os achados da semana."
            : "O seu mentor ainda não publicou listas. Você recebe um aviso quando sair a primeira."}
        </Card>
      )}

      <ul className="grid grid-cols-1 gap-3 md:grid-cols-2">
        {lists.data?.map((l) => (
          <li key={l.id}>
            <ListCard list={l} workspaceId={workspaceId} />
          </li>
        ))}
      </ul>
    </div>
  );
}

function ListCard({ list: l, workspaceId }: { list: CuratedList; workspaceId: string }) {
  return (
    <Link to="/w/$workspaceId/listas/$listId" params={{ workspaceId, listId: l.id }} className="block h-full">
      <Card className="flex h-full flex-col gap-2 p-4 hover:border-zinc-300">
        <div className="flex items-start justify-between gap-2">
          <p className="font-medium">{l.title}</p>
          {l.published_at ? (
            l.imported_by_me && <Badge className="bg-emerald-50 text-emerald-800">Importada</Badge>
          ) : (
            <Badge className="bg-amber-50 text-amber-800">Rascunho</Badge>
          )}
        </div>
        {l.description && <p className="line-clamp-2 text-sm text-muted">{l.description}</p>}
        <p className="mt-auto flex flex-wrap gap-x-3 text-xs text-muted">
          <span className="flex items-center gap-1">
            <ListChecks className="size-3.5" /> {l.products === 1 ? "1 produto" : `${l.products} produtos`}
          </span>
          {l.published_at ? <span>Publicada {timeAgo(l.published_at)}</span> : <span>Editada {timeAgo(l.updated_at)}</span>}
          {l.importers !== undefined && l.published_at && (
            <span>{l.importers === 1 ? "1 importação" : `${l.importers} importações`}</span>
          )}
        </p>
      </Card>
    </Link>
  );
}

function NewList({ workspaceId, onCancel }: { workspaceId: string; onCancel: () => void }) {
  const navigate = useNavigate();
  const create = useCreateList(workspaceId);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  return (
    <Card className="p-4">
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate(
            { title, description },
            {
              onSuccess: (l) => navigate({ to: "/w/$workspaceId/listas/$listId", params: { workspaceId, listId: l.id } }),
            },
          );
        }}
      >
        <Field label="Título">
          <Input
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            maxLength={120}
            placeholder="ex.: Achados da semana"
            autoFocus
            required
          />
        </Field>
        <Field label="Descrição (opcional)">
          <Textarea value={description} onChange={(e) => setDescription(e.target.value)} maxLength={2000} />
        </Field>
        {create.error && <p className="text-sm text-red-700">{create.error.message}</p>}
        <div className="flex gap-2">
          <Button type="submit" disabled={create.isPending}>
            {create.isPending ? "Criando…" : "Criar e adicionar produtos"}
          </Button>
          <Button type="button" variant="ghost" onClick={onCancel}>
            Cancelar
          </Button>
        </div>
      </form>
    </Card>
  );
}
