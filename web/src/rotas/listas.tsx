import { Link, useNavigate } from "@tanstack/react-router";
import { ListChecks, Plus } from "lucide-react";
import { useState } from "react";
import type { Lista } from "@/api/cliente";
import { Button } from "@/components/ui/button";
import { Aviso, Badge, Card } from "@/components/ui/card";
import { Input, Rotulo, Textarea } from "@/components/ui/input";
import { haQuanto } from "@/lib/formato";
import { useCriarLista, useListas, useWorkspaceAtual } from "./listas-api";
import { rotaListas } from "./router";

export function Listas() {
  const { workspaceId } = rotaListas.useParams();
  const { workspace, gestor } = useWorkspaceAtual(workspaceId);
  const listas = useListas(workspaceId);
  const [criando, setCriando] = useState(false);

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <h1 className="text-2xl font-semibold">Listas</h1>
          <p className="text-sm text-suave">
            {gestor
              ? "Monte listas de produtos com as suas dicas e publique para a turma."
              : "Produtos escolhidos pelo seu mentor. Importe para a sua coleção e receba o seu link."}
          </p>
        </div>
        {gestor && !criando && (
          <Button onClick={() => setCriando(true)}>
            <Plus className="size-4" /> Nova lista
          </Button>
        )}
      </div>

      {gestor && criando && <NovaLista workspaceId={workspaceId} onCancelar={() => setCriando(false)} />}

      {workspace?.tipo === "pessoal" && (
        <Aviso className="border-borda bg-white text-suave">
          As listas da curadoria ficam nos workspaces de mentoria. Escolha uma mentoria no seletor do topo, ou{" "}
          <Link to="/w/$workspaceId/turma" params={{ workspaceId }} className="text-marca hover:underline">
            crie a sua
          </Link>
          .
        </Aviso>
      )}
      {listas.error && <Aviso className="border-red-200 bg-red-50 text-red-800">{listas.error.message}</Aviso>}
      {listas.isPending && <p className="text-sm text-suave">Carregando listas…</p>}
      {listas.data?.length === 0 && workspace?.tipo === "mentoria" && (
        <Card className="p-8 text-center text-sm text-suave">
          {gestor
            ? "Nenhuma lista ainda. Crie a primeira com os achados da semana."
            : "O seu mentor ainda não publicou listas. Você recebe um aviso quando sair a primeira."}
        </Card>
      )}

      <ul className="grid grid-cols-1 gap-3 md:grid-cols-2">
        {listas.data?.map((l) => (
          <li key={l.id}>
            <CartaoLista lista={l} workspaceId={workspaceId} />
          </li>
        ))}
      </ul>
    </div>
  );
}

function CartaoLista({ lista: l, workspaceId }: { lista: Lista; workspaceId: string }) {
  return (
    <Link to="/w/$workspaceId/listas/$listaId" params={{ workspaceId, listaId: l.id }} className="block h-full">
      <Card className="flex h-full flex-col gap-2 p-4 hover:border-zinc-300">
        <div className="flex items-start justify-between gap-2">
          <p className="font-medium">{l.titulo}</p>
          {l.publicada_em ? (
            l.importei && <Badge className="bg-emerald-50 text-emerald-800">Importada</Badge>
          ) : (
            <Badge className="bg-amber-50 text-amber-800">Rascunho</Badge>
          )}
        </div>
        {l.descricao && <p className="line-clamp-2 text-sm text-suave">{l.descricao}</p>}
        <p className="mt-auto flex flex-wrap gap-x-3 text-xs text-suave">
          <span className="flex items-center gap-1">
            <ListChecks className="size-3.5" /> {l.produtos === 1 ? "1 produto" : `${l.produtos} produtos`}
          </span>
          {l.publicada_em ? <span>Publicada {haQuanto(l.publicada_em)}</span> : <span>Editada {haQuanto(l.atualizado_em)}</span>}
          {l.importadores !== undefined && l.publicada_em && (
            <span>{l.importadores === 1 ? "1 importação" : `${l.importadores} importações`}</span>
          )}
        </p>
      </Card>
    </Link>
  );
}

function NovaLista({ workspaceId, onCancelar }: { workspaceId: string; onCancelar: () => void }) {
  const navigate = useNavigate();
  const criar = useCriarLista(workspaceId);
  const [titulo, setTitulo] = useState("");
  const [descricao, setDescricao] = useState("");
  return (
    <Card className="p-4">
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          criar.mutate(
            { titulo, descricao },
            {
              onSuccess: (l) =>
                navigate({ to: "/w/$workspaceId/listas/$listaId", params: { workspaceId, listaId: l.id } }),
            },
          );
        }}
      >
        <Rotulo texto="Título">
          <Input
            value={titulo}
            onChange={(e) => setTitulo(e.target.value)}
            maxLength={120}
            placeholder="ex.: Achados da semana"
            autoFocus
            required
          />
        </Rotulo>
        <Rotulo texto="Descrição (opcional)">
          <Textarea value={descricao} onChange={(e) => setDescricao(e.target.value)} maxLength={2000} />
        </Rotulo>
        {criar.error && <p className="text-sm text-red-700">{criar.error.message}</p>}
        <div className="flex gap-2">
          <Button type="submit" disabled={criar.isPending}>
            {criar.isPending ? "Criando…" : "Criar e adicionar produtos"}
          </Button>
          <Button type="button" variante="fantasma" onClick={onCancelar}>
            Cancelar
          </Button>
        </div>
      </form>
    </Card>
  );
}
