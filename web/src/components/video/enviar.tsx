import AwsS3 from "@uppy/aws-s3";
import Uppy, { type Meta } from "@uppy/core";
import { Upload, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { api, exigir, type Video } from "@/api/cliente";
import { Button } from "@/components/ui/button";
import { tamanho } from "@/lib/formato";

const MB = 1024 * 1024;
export const maxBytesVideo = 1024 * MB;
const tipos: Record<string, string> = { mp4: "video/mp4", m4v: "video/mp4", mov: "video/quicktime", webm: "video/webm" };

/** Content-Type do arquivo; alguns celulares não informam o dos .mov. */
export function tipoDoVideo(f: { name: string; type?: string | null }): string {
  if (f.type && Object.values(tipos).includes(f.type)) return f.type;
  const ext = f.name.split(".").pop()?.toLowerCase() ?? "";
  return tipos[ext] ?? f.type ?? "";
}

type Estado = { fase: "parado" } | { fase: "enviando"; progresso: number; nome: string } | { fase: "erro"; mensagem: string };

/**
 * Envio de vídeo próprio com o Uppy: as partes vão direto do navegador ao
 * bucket por URLs pré-assinadas pela API, com retomada e novas tentativas.
 */
export function EnviarVideo({
  workspaceId,
  produtoId,
  onEnviado,
}: {
  workspaceId: string;
  produtoId?: string;
  onEnviado: (v: Video) => void;
}) {
  const [direito, setDireito] = useState(false);
  const [estado, setEstado] = useState<Estado>({ fase: "parado" });
  const entrada = useRef<HTMLInputElement>(null);
  const uppyRef = useRef<Uppy<Meta, Record<string, never>> | null>(null);
  const aoEnviar = useRef(onEnviado);
  aoEnviar.current = onEnviado;

  useEffect(() => {
    // O vídeo criado na API para cada arquivo do Uppy.
    const videos = new Map<string, string>();
    const concluidos = new Map<string, Video>();
    const caminho = (fileId: string) => ({ workspaceId, videoId: videos.get(fileId) ?? "" });

    const uppy = new Uppy<Meta, Record<string, never>>({
      autoProceed: true,
      restrictions: { maxNumberOfFiles: 1, maxFileSize: maxBytesVideo },
    }).use(AwsS3, {
      shouldUseMultipart: true,
      limit: 4,
      // Partes de 8 MB (o S3 pede ao menos 5 MB e no máximo 10 mil partes).
      getChunkSize: (f) => Math.max(8 * MB, Math.ceil(f.size / 10000)),
      async createMultipartUpload(file) {
        const r = await exigir(
          api.POST("/v1/workspaces/{workspaceId}/videos/uploads", {
            params: { path: { workspaceId } },
            body: {
              nome: file.name ?? "video",
              content_type: tipoDoVideo({ name: file.name ?? "", type: file.type }) as "video/mp4",
              tamanho: file.size ?? 0,
              direito_uso: true,
              ...(produtoId ? { produto_id: produtoId } : {}),
            },
          }),
        );
        videos.set(file.id, r.video.id);
        return { uploadId: r.upload_id, key: r.chave };
      },
      async signPart(file, { partNumber }) {
        const r = await exigir(
          api.POST("/v1/workspaces/{workspaceId}/videos/{videoId}/partes", {
            params: { path: caminho(file.id) },
            body: { numero: partNumber },
          }),
        );
        return { method: "PUT", url: r.url };
      },
      async listParts(file) {
        const ps = await exigir(
          api.GET("/v1/workspaces/{workspaceId}/videos/{videoId}/partes", { params: { path: caminho(file.id) } }),
        );
        return ps.map((p) => ({ PartNumber: p.numero, ETag: p.etag, Size: p.tamanho }));
      },
      async completeMultipartUpload(file, { parts }) {
        const v = await exigir(
          api.POST("/v1/workspaces/{workspaceId}/videos/{videoId}/concluir", {
            params: { path: caminho(file.id) },
            body: { partes: parts.map((p) => ({ numero: p.PartNumber ?? 0, etag: p.ETag ?? "" })) },
          }),
        );
        concluidos.set(file.id, v);
        return {};
      },
      async abortMultipartUpload(file) {
        if (!videos.has(file.id)) return;
        await api.DELETE("/v1/workspaces/{workspaceId}/videos/{videoId}", { params: { path: caminho(file.id) } });
      },
    });

    uppy.on("upload-progress", (file, p) => {
      if (!file || !p.bytesTotal) return;
      setEstado({ fase: "enviando", progresso: p.bytesUploaded / p.bytesTotal, nome: file.name ?? "" });
    });
    uppy.on("upload-error", (_file, erro) => {
      setEstado({ fase: "erro", mensagem: erro.message || "Não deu para enviar o vídeo. Tente de novo." });
    });
    uppy.on("restriction-failed", (_file, erro) => setEstado({ fase: "erro", mensagem: erro.message }));
    uppy.on("upload-success", (file) => {
      const v = file && concluidos.get(file.id);
      if (v) aoEnviar.current(v);
    });
    uppy.on("complete", () => {
      uppy.clear();
      setEstado((e) => (e.fase === "erro" ? e : { fase: "parado" }));
    });
    uppyRef.current = uppy;
    return () => {
      uppy.cancelAll();
      uppy.destroy();
      uppyRef.current = null;
    };
  }, [workspaceId, produtoId]);

  const escolher = (f: File | undefined) => {
    const uppy = uppyRef.current;
    if (!f || !uppy) return;
    if (!tipoDoVideo(f).startsWith("video/")) {
      setEstado({ fase: "erro", mensagem: "Envie um vídeo MP4, MOV ou WebM." });
      return;
    }
    if (f.size > maxBytesVideo) {
      setEstado({ fase: "erro", mensagem: `O vídeo pode ter até ${tamanho(maxBytesVideo)}.` });
      return;
    }
    uppy.clear();
    setEstado({ fase: "enviando", progresso: 0, nome: f.name });
    try {
      uppy.addFile({ name: f.name, type: tipoDoVideo(f), data: f, source: "local" });
    } catch (e) {
      setEstado({ fase: "erro", mensagem: e instanceof Error ? e.message : "Arquivo recusado." });
    }
  };

  if (estado.fase === "enviando") {
    const pct = Math.round(estado.progresso * 100);
    return (
      <div className="flex flex-col gap-2" aria-live="polite">
        <div className="flex items-center gap-2 text-sm">
          <span className="min-w-0 flex-1 truncate">{estado.nome}</span>
          <span className="tabular-nums text-suave">{pct}%</span>
          <Button
            type="button"
            variante="fantasma"
            tamanho="sm"
            aria-label="Cancelar envio"
            onClick={() => {
              uppyRef.current?.cancelAll();
              setEstado({ fase: "parado" });
            }}
          >
            <X className="size-4" />
          </Button>
        </div>
        <div className="h-2 overflow-hidden rounded-full bg-zinc-100" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
          <div className="h-full bg-marca transition-[width]" style={{ width: `${pct}%` }} />
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-2">
      <label className="flex items-start gap-2 text-sm">
        <input
          type="checkbox"
          className="mt-0.5 size-4 accent-marca"
          checked={direito}
          onChange={(e) => setDireito(e.target.checked)}
        />
        <span>Tenho direito de uso deste vídeo (gravei ou tenho autorização de quem gravou).</span>
      </label>
      <input
        ref={entrada}
        type="file"
        accept="video/mp4,video/quicktime,video/webm,.mp4,.mov,.webm"
        className="hidden"
        onChange={(e) => {
          escolher(e.target.files?.[0]);
          e.target.value = "";
        }}
      />
      <Button type="button" variante="secundario" disabled={!direito} onClick={() => entrada.current?.click()}>
        <Upload className="size-4" /> Escolher vídeo
      </Button>
      <p className="text-xs text-suave">MP4, MOV ou WebM, até {tamanho(maxBytesVideo)}.</p>
      {estado.fase === "erro" && <p className="text-sm text-red-700">{estado.mensagem}</p>}
    </div>
  );
}
