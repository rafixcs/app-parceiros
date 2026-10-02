import { Download, Pencil, Plus, Share2, Trash2 } from "lucide-react";
import { useState } from "react";
import type { Video } from "@/api/cliente";
import { Button } from "@/components/ui/button";
import { Aviso, Badge, Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { AdicionarVideo } from "@/components/video/adicionar";
import { LegendaVideo, PlayerVideo } from "@/components/video/player";
import { tamanho } from "@/lib/formato";
import { cn } from "@/lib/utils";
import { useWorkspaceAtual } from "./listas-api";
import { rotaVideos } from "./router";
import { baixarVideo, useApagarVideo, useCotaVideos, useEditarVideo, useVideos } from "./videos-api";

type Filtro = "todos" | "meus" | "turma";

export function Videos() {
  const { workspaceId } = rotaVideos.useParams();
  const { workspace, gestor } = useWorkspaceAtual(workspaceId);
  const videos = useVideos(workspaceId);
  const [filtro, setFiltro] = useState<Filtro>("todos");
  const [adicionando, setAdicionando] = useState(false);
  const mentoria = workspace?.tipo === "mentoria";

  const lista = (videos.data ?? []).filter((v) => filtro === "todos" || (filtro === "meus" ? v.meu : !v.meu));

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-wrap items-end gap-3">
        <div className="mr-auto">
          <h1 className="text-2xl font-semibold">Vídeos</h1>
          <p className="text-sm text-suave">
            Referências de outros criadores e os seus vídeos, para ligar aos produtos
            {gestor ? " e anexar às listas da turma" : ""}.
          </p>
        </div>
        <Button onClick={() => setAdicionando((a) => !a)} variante={adicionando ? "secundario" : "primario"}>
          <Plus className="size-4" /> Adicionar vídeo
        </Button>
      </div>

      <div className="grid gap-4 md:grid-cols-[1fr_18rem]">
        {adicionando ? (
          <Card className="p-4">
            <AdicionarVideo workspaceId={workspaceId} onAdicionado={() => setAdicionando(false)} />
          </Card>
        ) : (
          <div className="hidden md:block" />
        )}
        <Cota workspaceId={workspaceId} />
      </div>

      {mentoria && (
        <div className="flex gap-1" role="tablist" aria-label="Filtrar vídeos">
          {(
            [
              ["todos", "Todos"],
              ["meus", "Meus"],
              ["turma", gestor ? "De outros mentores" : "Do mentor"],
            ] as const
          ).map(([f, rotulo]) => (
            <Button key={f} tamanho="sm" variante={filtro === f ? "primario" : "secundario"} onClick={() => setFiltro(f)}>
              {rotulo}
            </Button>
          ))}
        </div>
      )}

      {videos.error && <Aviso className="border-red-200 bg-red-50 text-red-800">{videos.error.message}</Aviso>}
      {videos.isPending && <p className="text-sm text-suave">Carregando vídeos…</p>}
      {videos.data && lista.length === 0 && (
        <Card className="p-8 text-center text-sm text-suave">
          {filtro === "todos"
            ? "Nenhum vídeo ainda. Cole o link de um vídeo do TikTok ou do YouTube, ou envie um vídeo seu."
            : "Nenhum vídeo aqui."}
        </Card>
      )}
      <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {lista.map((v) => (
          <li key={v.id}>
            <CartaoVideo video={v} workspaceId={workspaceId} podeCompartilhar={gestor} />
          </li>
        ))}
      </ul>
    </div>
  );
}

function Cota({ workspaceId }: { workspaceId: string }) {
  const cota = useCotaVideos(workspaceId);
  if (!cota.data) return null;
  const { usados_bytes: usados, limite_bytes: limite } = cota.data;
  const pct = limite > 0 ? Math.min(100, Math.round((usados / limite) * 100)) : 100;
  return (
    <Card className="flex flex-col gap-2 self-start p-4">
      <p className="text-sm font-medium">Espaço para vídeos enviados</p>
      <div className="h-2 overflow-hidden rounded-full bg-zinc-100">
        <div className={cn("h-full", pct >= 90 ? "bg-red-500" : "bg-marca")} style={{ width: `${pct}%` }} />
      </div>
      <p className="text-xs text-suave">
        {tamanho(usados)} de {tamanho(limite)} usados
      </p>
    </Card>
  );
}

/** Cartão do vídeo com o player e as ações de quem o vê. */
export function CartaoVideo({
  video: v,
  workspaceId,
  podeCompartilhar,
  acoes,
}: {
  video: Video;
  workspaceId: string;
  podeCompartilhar?: boolean;
  acoes?: React.ReactNode;
}) {
  const editar = useEditarVideo(workspaceId);
  const apagar = useApagarVideo(workspaceId);
  const [renomeando, setRenomeando] = useState(false);
  const [titulo, setTitulo] = useState(v.titulo);
  const [erroDownload, setErroDownload] = useState<string | null>(null);
  const erro = editar.error?.message ?? apagar.error?.message ?? erroDownload;
  const baixavel = v.tipo === "upload" && (v.status === "pronto" || v.status === "falhou");

  return (
    <Card className="flex h-full flex-col gap-3 p-3">
      <PlayerVideo video={v} className="mx-auto max-h-96 w-full" />
      {renomeando ? (
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            editar.mutate({ id: v.id, titulo }, { onSuccess: () => setRenomeando(false) });
          }}
        >
          <Input aria-label="Título do vídeo" value={titulo} maxLength={200} onChange={(e) => setTitulo(e.target.value)} autoFocus />
          <Button type="submit" tamanho="sm" disabled={editar.isPending}>
            Salvar
          </Button>
        </form>
      ) : (
        <div className="flex items-start gap-2">
          <LegendaVideo video={v} />
          {v.compartilhado && v.meu && (
            <Badge className="ml-auto shrink-0 bg-emerald-50 text-emerald-800">
              <Share2 className="size-3" /> Turma
            </Badge>
          )}
        </div>
      )}
      <div className="mt-auto flex flex-wrap items-center gap-1">
        {v.url && (
          <a href={v.url} target="_blank" rel="noreferrer" className="text-xs text-marca hover:underline">
            Abrir no {v.plataforma === "tiktok" ? "TikTok" : "YouTube"}
          </a>
        )}
        {baixavel && (
          <Button
            variante="fantasma"
            tamanho="sm"
            onClick={() => {
              setErroDownload(null);
              baixarVideo(workspaceId, v.id).catch((e: Error) => setErroDownload(e.message));
            }}
          >
            <Download className="size-4" /> Baixar
          </Button>
        )}
        {acoes}
        {v.meu && (
          <span className="ml-auto flex gap-1">
            {podeCompartilhar && (
              <Button
                variante="fantasma"
                tamanho="sm"
                aria-pressed={v.compartilhado}
                title={v.compartilhado ? "Parar de compartilhar com a turma" : "Compartilhar com a turma"}
                disabled={editar.isPending}
                onClick={() => editar.mutate({ id: v.id, compartilhado: !v.compartilhado })}
              >
                <Share2 className={cn("size-4", v.compartilhado && "text-emerald-700")} />
                <span className="sr-only">Compartilhar com a turma</span>
              </Button>
            )}
            <Button variante="fantasma" tamanho="sm" aria-label="Renomear" onClick={() => setRenomeando((r) => !r)}>
              <Pencil className="size-4" />
            </Button>
            <Button
              variante="fantasma"
              tamanho="sm"
              aria-label="Apagar vídeo"
              disabled={apagar.isPending}
              onClick={() => {
                const aviso =
                  v.tipo === "upload"
                    ? "Apagar este vídeo? O arquivo sai do app e dos produtos e listas em que ele aparece."
                    : "Tirar esta referência da sua biblioteca, dos produtos e das listas?";
                if (confirm(aviso)) apagar.mutate(v.id);
              }}
            >
              <Trash2 className="size-4" />
            </Button>
          </span>
        )}
      </div>
      {erro && <p className="text-sm text-red-700">{erro}</p>}
    </Card>
  );
}
