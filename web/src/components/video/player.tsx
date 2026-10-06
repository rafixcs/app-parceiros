import { AlertTriangle, Loader2, Play } from "lucide-react";
import { useState } from "react";
import type { Video } from "@/api/client";
import { duration } from "@/lib/format";
import { cn } from "@/lib/utils";

const platforms: Record<Video["platform"], string> = {
  youtube: "YouTube",
  tiktok: "TikTok",
  upload: "Vídeo próprio",
};

export function platformName(v: Video) {
  return platforms[v.platform];
}

/** A portrait video (TikTok and vertical uploads) takes the 9:16 ratio. */
export function isPortrait(v: Video) {
  if (v.platform === "tiktok") return true;
  return v.kind === "upload" && v.width != null && v.height != null && v.height > v.width;
}

/**
 * The video player. References show the thumbnail and only load the official
 * player (iframe) on click; own videos play the 720p preview.
 */
export function VideoPlayer({ video: v, className }: { video: Video; className?: string }) {
  const [playing, setPlaying] = useState(false);
  // The signed URL changes on every query; the first one serves while the
  // player is open, so the video does not restart.
  const [preview, setPreview] = useState(v.preview_url);
  const [thumbnailFailed, setThumbnailFailed] = useState(false);
  const ratio = isPortrait(v) ? "aspect-[9/16]" : "aspect-video";
  const box = cn("relative overflow-hidden rounded-lg bg-zinc-900", ratio, className);

  if (v.status === "uploading" || v.status === "processing") {
    return (
      <div className={cn(box, "flex flex-col items-center justify-center gap-2 text-sm text-zinc-200")}>
        <Loader2 className="size-6 animate-spin" />
        {v.status === "uploading" ? "Enviando…" : "Processando o vídeo…"}
      </div>
    );
  }
  if (v.status === "failed" || v.status === "unavailable") {
    return (
      <div className={cn(box, "flex flex-col items-center justify-center gap-2 p-3 text-center text-sm text-zinc-200")}>
        <AlertTriangle className="size-6 text-amber-400" />
        {v.status === "failed"
          ? "Não conseguimos ler este arquivo como vídeo."
          : `O vídeo não está mais disponível no ${platformName(v)}.`}
      </div>
    );
  }

  if (v.kind === "upload") {
    const src = preview ?? v.preview_url;
    return (
      <div className={box}>
        <video
          className="size-full object-contain"
          controls
          playsInline
          preload="none"
          poster={v.thumbnail_url ?? undefined}
          src={src ?? undefined}
          onError={() => setPreview(v.preview_url)}
        >
          <track kind="captions" />
        </video>
      </div>
    );
  }

  if (playing && v.player_url) {
    const sep = v.player_url.includes("?") ? "&" : "?";
    return (
      <div className={box}>
        <iframe
          className="size-full"
          src={`${v.player_url}${sep}autoplay=1`}
          title={v.title || `Vídeo do ${platformName(v)}`}
          allow="autoplay; encrypted-media; picture-in-picture; fullscreen"
          allowFullScreen
          referrerPolicy="strict-origin-when-cross-origin"
        />
      </div>
    );
  }
  return (
    <button
      type="button"
      className={cn(box, "group block w-full")}
      onClick={() => setPlaying(true)}
      aria-label={`Tocar ${v.title || "vídeo"} no player do ${platformName(v)}`}
    >
      {v.thumbnail_url && !thumbnailFailed && (
        <img
          src={v.thumbnail_url}
          alt=""
          loading="lazy"
          referrerPolicy="no-referrer"
          onError={() => setThumbnailFailed(true)}
          className="size-full object-cover opacity-90 transition group-hover:opacity-100"
        />
      )}
      <span className="absolute inset-0 flex items-center justify-center">
        <span className="flex size-12 items-center justify-center rounded-full bg-black/60 text-white transition group-hover:bg-brand">
          <Play className="size-5 fill-current" />
        </span>
      </span>
      <span className="absolute bottom-2 left-2 rounded bg-black/60 px-1.5 py-0.5 text-[11px] font-medium text-white">
        {platformName(v)}
      </span>
    </button>
  );
}

/** A line with the video data: title, author or duration. */
export function VideoCaption({ video: v }: { video: Video }) {
  const details = [
    v.kind === "embed" ? v.author : null,
    v.duration_s != null ? duration(v.duration_s) : null,
    !v.mine ? "do mentor" : null,
  ].filter(Boolean);
  return (
    <div className="min-w-0">
      <p className="line-clamp-2 text-sm font-medium leading-snug">{v.title || "Sem título"}</p>
      {details.length > 0 && <p className="truncate text-xs text-muted">{details.join(" · ")}</p>}
    </div>
  );
}
