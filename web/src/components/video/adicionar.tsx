import { Link2, Upload } from "lucide-react";
import { lazy, Suspense, useState } from "react";
import type { Video } from "@/api/cliente";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { useColarVideo, useInvalidarVideos } from "@/rotas/videos-api";

// O Uppy só carrega quando alguém vai enviar um arquivo.
const EnviarVideo = lazy(() => import("./enviar").then((m) => ({ default: m.EnviarVideo })));

/** Adiciona um vídeo: referência por link (YouTube ou TikTok) ou arquivo próprio. */
export function AdicionarVideo({
  workspaceId,
  produtoId,
  onAdicionado,
}: {
  workspaceId: string;
  produtoId?: string;
  onAdicionado?: (v: Video) => void;
}) {
  const [modo, setModo] = useState<"link" | "arquivo">("link");
  const [link, setLink] = useState("");
  const colar = useColarVideo(workspaceId, produtoId);
  const invalidar = useInvalidarVideos(workspaceId);
  const aba = "flex h-8 flex-1 items-center justify-center gap-1.5 rounded-md text-sm";

  return (
    <div className="flex flex-col gap-3">
      <div className="flex gap-1 rounded-lg bg-zinc-100 p-1" role="tablist">
        <button
          type="button"
          role="tab"
          aria-selected={modo === "link"}
          className={cn(aba, modo === "link" && "bg-white font-medium shadow-sm")}
          onClick={() => setModo("link")}
        >
          <Link2 className="size-4" /> Colar link
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={modo === "arquivo"}
          className={cn(aba, modo === "arquivo" && "bg-white font-medium shadow-sm")}
          onClick={() => setModo("arquivo")}
        >
          <Upload className="size-4" /> Enviar arquivo
        </button>
      </div>
      {modo === "link" ? (
        <form
          className="flex flex-col gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            colar.mutate(link.trim(), {
              onSuccess: (v) => {
                setLink("");
                onAdicionado?.(v);
              },
            });
          }}
        >
          <div className="flex gap-2">
            <Input
              aria-label="Link do vídeo"
              placeholder="https://www.tiktok.com/@... ou https://youtu.be/..."
              value={link}
              onChange={(e) => setLink(e.target.value)}
            />
            <Button type="submit" disabled={colar.isPending || !link.trim()}>
              {colar.isPending ? "Buscando…" : "Adicionar"}
            </Button>
          </div>
          <p className="text-xs text-suave">
            Vídeos de outros criadores ficam só como referência: guardamos título, autor e miniatura, e o vídeo toca no
            player oficial.
          </p>
          {colar.error && <p className="text-sm text-red-700">{colar.error.message}</p>}
        </form>
      ) : (
        <Suspense fallback={<p className="text-sm text-suave">Carregando…</p>}>
          <EnviarVideo
            workspaceId={workspaceId}
            produtoId={produtoId}
            onEnviado={(v) => {
              invalidar();
              onAdicionado?.(v);
            }}
          />
        </Suspense>
      )}
    </div>
  );
}
