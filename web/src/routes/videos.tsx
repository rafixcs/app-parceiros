import { Download, Pencil, Plus, Share2, Trash2 } from "lucide-react";
import { useState } from "react";
import type { Video } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Badge, Card, Notice } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { AddVideo } from "@/components/video/add";
import { VideoCaption, VideoPlayer } from "@/components/video/player";
import { fileSize } from "@/lib/format";
import { cn } from "@/lib/utils";
import { useCurrentWorkspace } from "./layout";
import { videosRoute } from "./router";
import { downloadVideo, useDeleteVideo, useUpdateVideo, useVideoQuota, useVideos } from "./videos-api";

type Filter = "all" | "mine" | "cohort";

export function Videos() {
  const { workspaceId } = videosRoute.useParams();
  const { workspace, manager } = useCurrentWorkspace(workspaceId);
  const videos = useVideos(workspaceId);
  const [filter, setFilter] = useState<Filter>("all");
  const [adding, setAdding] = useState(false);
  const mentorship = workspace?.kind === "mentorship";

  const shown = (videos.data ?? []).filter((v) => filter === "all" || (filter === "mine" ? v.mine : !v.mine));

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-wrap items-end gap-3">
        <div className="mr-auto">
          <h1 className="text-2xl font-semibold">Vídeos</h1>
          <p className="text-sm text-muted">
            Referências de outros criadores e os seus vídeos, para ligar aos produtos
            {manager ? " e anexar às listas da turma" : ""}.
          </p>
        </div>
        <Button onClick={() => setAdding((a) => !a)} variant={adding ? "secondary" : "primary"}>
          <Plus className="size-4" /> Adicionar vídeo
        </Button>
      </div>

      <div className="grid gap-4 md:grid-cols-[1fr_18rem]">
        {adding ? (
          <Card className="p-4">
            <AddVideo workspaceId={workspaceId} onAdded={() => setAdding(false)} />
          </Card>
        ) : (
          <div className="hidden md:block" />
        )}
        <Quota workspaceId={workspaceId} />
      </div>

      {mentorship && (
        <div className="flex gap-1" role="tablist" aria-label="Filtrar vídeos">
          {(
            [
              ["all", "Todos"],
              ["mine", "Meus"],
              ["cohort", manager ? "De outros mentores" : "Do mentor"],
            ] as const
          ).map(([f, label]) => (
            <Button key={f} size="sm" variant={filter === f ? "primary" : "secondary"} onClick={() => setFilter(f)}>
              {label}
            </Button>
          ))}
        </div>
      )}

      {videos.error && <Notice className="border-red-200 bg-red-50 text-red-800">{videos.error.message}</Notice>}
      {videos.isPending && <p className="text-sm text-muted">Carregando vídeos…</p>}
      {videos.data && shown.length === 0 && (
        <Card className="p-8 text-center text-sm text-muted">
          {filter === "all"
            ? "Nenhum vídeo ainda. Cole o link de um vídeo do TikTok ou do YouTube, ou envie um vídeo seu."
            : "Nenhum vídeo aqui."}
        </Card>
      )}
      <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {shown.map((v) => (
          <li key={v.id}>
            <VideoCard video={v} workspaceId={workspaceId} canShare={manager} />
          </li>
        ))}
      </ul>
    </div>
  );
}

function Quota({ workspaceId }: { workspaceId: string }) {
  const quota = useVideoQuota(workspaceId);
  if (!quota.data) return null;
  const { used_bytes: used, limit_bytes: limit } = quota.data;
  const pct = limit > 0 ? Math.min(100, Math.round((used / limit) * 100)) : 100;
  return (
    <Card className="flex flex-col gap-2 self-start p-4">
      <p className="text-sm font-medium">Espaço para vídeos enviados</p>
      <div className="h-2 overflow-hidden rounded-full bg-zinc-100">
        <div className={cn("h-full", pct >= 90 ? "bg-red-500" : "bg-brand")} style={{ width: `${pct}%` }} />
      </div>
      <p className="text-xs text-muted">
        {fileSize(used)} de {fileSize(limit)} usados
      </p>
    </Card>
  );
}

/** The video card with the player and the viewer's actions. */
export function VideoCard({
  video: v,
  workspaceId,
  canShare,
  actions,
}: {
  video: Video;
  workspaceId: string;
  canShare?: boolean;
  actions?: React.ReactNode;
}) {
  const update = useUpdateVideo(workspaceId);
  const remove = useDeleteVideo(workspaceId);
  const [renaming, setRenaming] = useState(false);
  const [title, setTitle] = useState(v.title);
  const [downloadError, setDownloadError] = useState<string | null>(null);
  const error = update.error?.message ?? remove.error?.message ?? downloadError;
  const downloadable = v.kind === "upload" && (v.status === "ready" || v.status === "failed");

  return (
    <Card className="flex h-full flex-col gap-3 p-3">
      <VideoPlayer video={v} className="mx-auto max-h-96 w-full" />
      {renaming ? (
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            update.mutate({ id: v.id, title }, { onSuccess: () => setRenaming(false) });
          }}
        >
          <Input aria-label="Título do vídeo" value={title} maxLength={200} onChange={(e) => setTitle(e.target.value)} autoFocus />
          <Button type="submit" size="sm" disabled={update.isPending}>
            Salvar
          </Button>
        </form>
      ) : (
        <div className="flex items-start gap-2">
          <VideoCaption video={v} />
          {v.shared && v.mine && (
            <Badge className="ml-auto shrink-0 bg-emerald-50 text-emerald-800">
              <Share2 className="size-3" /> Turma
            </Badge>
          )}
        </div>
      )}
      <div className="mt-auto flex flex-wrap items-center gap-1">
        {v.url && (
          <a href={v.url} target="_blank" rel="noreferrer" className="text-xs text-brand hover:underline">
            Abrir no {v.platform === "tiktok" ? "TikTok" : "YouTube"}
          </a>
        )}
        {downloadable && (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              setDownloadError(null);
              downloadVideo(workspaceId, v.id).catch((e: Error) => setDownloadError(e.message));
            }}
          >
            <Download className="size-4" /> Baixar
          </Button>
        )}
        {actions}
        {v.mine && (
          <span className="ml-auto flex gap-1">
            {canShare && (
              <Button
                variant="ghost"
                size="sm"
                aria-pressed={v.shared}
                title={v.shared ? "Parar de compartilhar com a turma" : "Compartilhar com a turma"}
                disabled={update.isPending}
                onClick={() => update.mutate({ id: v.id, shared: !v.shared })}
              >
                <Share2 className={cn("size-4", v.shared && "text-emerald-700")} />
                <span className="sr-only">Compartilhar com a turma</span>
              </Button>
            )}
            <Button variant="ghost" size="sm" aria-label="Renomear" onClick={() => setRenaming((r) => !r)}>
              <Pencil className="size-4" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              aria-label="Apagar vídeo"
              disabled={remove.isPending}
              onClick={() => {
                const warning =
                  v.kind === "upload"
                    ? "Apagar este vídeo? O arquivo sai do app e dos produtos e listas em que ele aparece."
                    : "Tirar esta referência da sua biblioteca, dos produtos e das listas?";
                if (confirm(warning)) remove.mutate(v.id);
              }}
            >
              <Trash2 className="size-4" />
            </Button>
          </span>
        )}
      </div>
      {error && <p className="text-sm text-red-700">{error}</p>}
    </Card>
  );
}
