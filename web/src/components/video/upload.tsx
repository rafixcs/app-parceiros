import AwsS3 from "@uppy/aws-s3";
import Uppy, { type Meta } from "@uppy/core";
import { Upload, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { api, unwrap, type Video } from "@/api/client";
import { Button } from "@/components/ui/button";
import { fileSize } from "@/lib/format";

const MB = 1024 * 1024;
export const maxVideoBytes = 1024 * MB;
const contentTypes: Record<string, string> = { mp4: "video/mp4", m4v: "video/mp4", mov: "video/quicktime", webm: "video/webm" };

/** Content-Type of the file; some phones do not report the one of .mov files. */
export function videoContentType(f: { name: string; type?: string | null }): string {
  if (f.type && Object.values(contentTypes).includes(f.type)) return f.type;
  const ext = f.name.split(".").pop()?.toLowerCase() ?? "";
  return contentTypes[ext] ?? f.type ?? "";
}

type State = { phase: "idle" } | { phase: "uploading"; progress: number; name: string } | { phase: "error"; message: string };

/**
 * Uploads an own video with Uppy: the parts go straight from the browser to
 * the bucket through URLs presigned by the API, with resume and retries.
 */
export function UploadVideo({
  workspaceId,
  productId,
  onUploaded,
}: {
  workspaceId: string;
  productId?: string;
  onUploaded: (v: Video) => void;
}) {
  const [rights, setRights] = useState(false);
  const [state, setState] = useState<State>({ phase: "idle" });
  const input = useRef<HTMLInputElement>(null);
  const uppyRef = useRef<Uppy<Meta, Record<string, never>> | null>(null);
  const uploaded = useRef(onUploaded);
  uploaded.current = onUploaded;

  useEffect(() => {
    // The video created in the API for each Uppy file.
    const videos = new Map<string, string>();
    const completed = new Map<string, Video>();
    const path = (fileId: string) => ({ workspaceId, videoId: videos.get(fileId) ?? "" });

    const uppy = new Uppy<Meta, Record<string, never>>({
      autoProceed: true,
      restrictions: { maxNumberOfFiles: 1, maxFileSize: maxVideoBytes },
    }).use(AwsS3, {
      shouldUseMultipart: true,
      limit: 4,
      // 8 MB parts (S3 asks for at least 5 MB and at most 10 thousand parts).
      getChunkSize: (f) => Math.max(8 * MB, Math.ceil(f.size / 10000)),
      async createMultipartUpload(file) {
        const r = await unwrap(
          api.POST("/v1/workspaces/{workspaceId}/videos/uploads", {
            params: { path: { workspaceId } },
            body: {
              file_name: file.name ?? "video",
              content_type: videoContentType({ name: file.name ?? "", type: file.type }) as "video/mp4",
              size_bytes: file.size ?? 0,
              usage_rights: true,
              ...(productId ? { product_id: productId } : {}),
            },
          }),
        );
        videos.set(file.id, r.video.id);
        return { uploadId: r.upload_id, key: r.key };
      },
      async signPart(file, { partNumber }) {
        const r = await unwrap(
          api.POST("/v1/workspaces/{workspaceId}/videos/{videoId}/parts", {
            params: { path: path(file.id) },
            body: { number: partNumber },
          }),
        );
        return { method: "PUT", url: r.url };
      },
      async listParts(file) {
        const ps = await unwrap(
          api.GET("/v1/workspaces/{workspaceId}/videos/{videoId}/parts", { params: { path: path(file.id) } }),
        );
        return ps.map((p) => ({ PartNumber: p.number, ETag: p.etag, Size: p.size }));
      },
      async completeMultipartUpload(file, { parts }) {
        const v = await unwrap(
          api.POST("/v1/workspaces/{workspaceId}/videos/{videoId}/complete", {
            params: { path: path(file.id) },
            body: { parts: parts.map((p) => ({ number: p.PartNumber ?? 0, etag: p.ETag ?? "" })) },
          }),
        );
        completed.set(file.id, v);
        return {};
      },
      async abortMultipartUpload(file) {
        if (!videos.has(file.id)) return;
        await api.DELETE("/v1/workspaces/{workspaceId}/videos/{videoId}", { params: { path: path(file.id) } });
      },
    });

    uppy.on("upload-progress", (file, p) => {
      if (!file || !p.bytesTotal) return;
      setState({ phase: "uploading", progress: p.bytesUploaded / p.bytesTotal, name: file.name ?? "" });
    });
    uppy.on("upload-error", (_file, err) => {
      setState({ phase: "error", message: err.message || "Não deu para enviar o vídeo. Tente de novo." });
    });
    uppy.on("restriction-failed", (_file, err) => setState({ phase: "error", message: err.message }));
    uppy.on("upload-success", (file) => {
      const v = file && completed.get(file.id);
      if (v) uploaded.current(v);
    });
    uppy.on("complete", () => {
      uppy.clear();
      setState((s) => (s.phase === "error" ? s : { phase: "idle" }));
    });
    uppyRef.current = uppy;
    return () => {
      uppy.cancelAll();
      uppy.destroy();
      uppyRef.current = null;
    };
  }, [workspaceId, productId]);

  const choose = (f: File | undefined) => {
    const uppy = uppyRef.current;
    if (!f || !uppy) return;
    if (!videoContentType(f).startsWith("video/")) {
      setState({ phase: "error", message: "Envie um vídeo MP4, MOV ou WebM." });
      return;
    }
    if (f.size > maxVideoBytes) {
      setState({ phase: "error", message: `O vídeo pode ter até ${fileSize(maxVideoBytes)}.` });
      return;
    }
    uppy.clear();
    setState({ phase: "uploading", progress: 0, name: f.name });
    try {
      uppy.addFile({ name: f.name, type: videoContentType(f), data: f, source: "local" });
    } catch (e) {
      setState({ phase: "error", message: e instanceof Error ? e.message : "Arquivo recusado." });
    }
  };

  if (state.phase === "uploading") {
    const pct = Math.round(state.progress * 100);
    return (
      <div className="flex flex-col gap-2" aria-live="polite">
        <div className="flex items-center gap-2 text-sm">
          <span className="min-w-0 flex-1 truncate">{state.name}</span>
          <span className="tabular-nums text-muted">{pct}%</span>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            aria-label="Cancelar envio"
            onClick={() => {
              uppyRef.current?.cancelAll();
              setState({ phase: "idle" });
            }}
          >
            <X className="size-4" />
          </Button>
        </div>
        <div className="h-2 overflow-hidden rounded-full bg-zinc-100" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
          <div className="h-full bg-brand transition-[width]" style={{ width: `${pct}%` }} />
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-2">
      <label className="flex items-start gap-2 text-sm">
        <input
          type="checkbox"
          className="mt-0.5 size-4 accent-brand"
          checked={rights}
          onChange={(e) => setRights(e.target.checked)}
        />
        <span>Tenho direito de uso deste vídeo (gravei ou tenho autorização de quem gravou).</span>
      </label>
      <input
        ref={input}
        type="file"
        accept="video/mp4,video/quicktime,video/webm,.mp4,.mov,.webm"
        className="hidden"
        onChange={(e) => {
          choose(e.target.files?.[0]);
          e.target.value = "";
        }}
      />
      <Button type="button" variant="secondary" disabled={!rights} onClick={() => input.current?.click()}>
        <Upload className="size-4" /> Escolher vídeo
      </Button>
      <p className="text-xs text-muted">MP4, MOV ou WebM, até {fileSize(maxVideoBytes)}.</p>
      {state.phase === "error" && <p className="text-sm text-red-700">{state.message}</p>}
    </div>
  );
}
