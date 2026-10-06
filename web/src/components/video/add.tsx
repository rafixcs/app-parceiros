import { Link2, Upload } from "lucide-react";
import { lazy, Suspense, useState } from "react";
import type { Video } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { useInvalidateVideos, usePasteVideo } from "@/routes/videos-api";

// Uppy only loads when someone is about to upload a file.
const UploadVideo = lazy(() => import("./upload").then((m) => ({ default: m.UploadVideo })));

/** Adds a video: a reference by link (YouTube or TikTok) or an own file. */
export function AddVideo({
  workspaceId,
  productId,
  onAdded,
}: {
  workspaceId: string;
  productId?: string;
  onAdded?: (v: Video) => void;
}) {
  const [mode, setMode] = useState<"link" | "file">("link");
  const [link, setLink] = useState("");
  const paste = usePasteVideo(workspaceId, productId);
  const invalidate = useInvalidateVideos(workspaceId);
  const tab = "flex h-8 flex-1 items-center justify-center gap-1.5 rounded-md text-sm";

  return (
    <div className="flex flex-col gap-3">
      <div className="flex gap-1 rounded-lg bg-zinc-100 p-1" role="tablist">
        <button
          type="button"
          role="tab"
          aria-selected={mode === "link"}
          className={cn(tab, mode === "link" && "bg-white font-medium shadow-sm")}
          onClick={() => setMode("link")}
        >
          <Link2 className="size-4" /> Colar link
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={mode === "file"}
          className={cn(tab, mode === "file" && "bg-white font-medium shadow-sm")}
          onClick={() => setMode("file")}
        >
          <Upload className="size-4" /> Enviar arquivo
        </button>
      </div>
      {mode === "link" ? (
        <form
          className="flex flex-col gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            paste.mutate(link.trim(), {
              onSuccess: (v) => {
                setLink("");
                onAdded?.(v);
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
            <Button type="submit" disabled={paste.isPending || !link.trim()}>
              {paste.isPending ? "Buscando…" : "Adicionar"}
            </Button>
          </div>
          <p className="text-xs text-muted">
            Vídeos de outros criadores ficam só como referência: guardamos título, autor e miniatura, e o vídeo toca no
            player oficial.
          </p>
          {paste.error && <p className="text-sm text-red-700">{paste.error.message}</p>}
        </form>
      ) : (
        <Suspense fallback={<p className="text-sm text-muted">Carregando…</p>}>
          <UploadVideo
            workspaceId={workspaceId}
            productId={productId}
            onUploaded={(v) => {
              invalidate();
              onAdded?.(v);
            }}
          />
        </Suspense>
      )}
    </div>
  );
}
