import { AlertTriangle, Loader2, Play } from "lucide-react";
import { useState } from "react";
import type { Video } from "@/api/cliente";
import { duracao } from "@/lib/formato";
import { cn } from "@/lib/utils";

const plataformas: Record<Video["plataforma"], string> = {
  youtube: "YouTube",
  tiktok: "TikTok",
  upload: "Vídeo próprio",
};

export function nomePlataforma(v: Video) {
  return plataformas[v.plataforma];
}

/** Vídeo em pé (TikTok e uploads verticais) ocupa a proporção 9:16. */
export function emPe(v: Video) {
  if (v.plataforma === "tiktok") return true;
  return v.tipo === "upload" && v.largura != null && v.altura != null && v.altura > v.largura;
}

/**
 * Player do vídeo. Referências mostram a miniatura e só carregam o player
 * oficial (iframe) no clique; vídeos próprios tocam a prévia em 720p.
 */
export function PlayerVideo({ video: v, className }: { video: Video; className?: string }) {
  const [tocando, setTocando] = useState(false);
  // A URL assinada muda a cada consulta; a primeira serve enquanto o player
  // estiver aberto, para não reiniciar o vídeo.
  const [previa, setPrevia] = useState(v.preview_url);
  const [thumbFalhou, setThumbFalhou] = useState(false);
  const proporcao = emPe(v) ? "aspect-[9/16]" : "aspect-video";
  const caixa = cn("relative overflow-hidden rounded-lg bg-zinc-900", proporcao, className);

  if (v.status === "enviando" || v.status === "processando") {
    return (
      <div className={cn(caixa, "flex flex-col items-center justify-center gap-2 text-sm text-zinc-200")}>
        <Loader2 className="size-6 animate-spin" />
        {v.status === "enviando" ? "Enviando…" : "Processando o vídeo…"}
      </div>
    );
  }
  if (v.status === "falhou" || v.status === "indisponivel") {
    return (
      <div className={cn(caixa, "flex flex-col items-center justify-center gap-2 p-3 text-center text-sm text-zinc-200")}>
        <AlertTriangle className="size-6 text-amber-400" />
        {v.status === "falhou"
          ? "Não conseguimos ler este arquivo como vídeo."
          : `O vídeo não está mais disponível no ${nomePlataforma(v)}.`}
      </div>
    );
  }

  if (v.tipo === "upload") {
    const src = previa ?? v.preview_url;
    return (
      <div className={caixa}>
        <video
          className="size-full object-contain"
          controls
          playsInline
          preload="none"
          poster={v.thumb_url ?? undefined}
          src={src ?? undefined}
          onError={() => setPrevia(v.preview_url)}
        >
          <track kind="captions" />
        </video>
      </div>
    );
  }

  if (tocando && v.player_url) {
    const sep = v.player_url.includes("?") ? "&" : "?";
    return (
      <div className={caixa}>
        <iframe
          className="size-full"
          src={`${v.player_url}${sep}autoplay=1`}
          title={v.titulo || `Vídeo do ${nomePlataforma(v)}`}
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
      className={cn(caixa, "group block w-full")}
      onClick={() => setTocando(true)}
      aria-label={`Tocar ${v.titulo || "vídeo"} no player do ${nomePlataforma(v)}`}
    >
      {v.thumb_url && !thumbFalhou && (
        <img
          src={v.thumb_url}
          alt=""
          loading="lazy"
          referrerPolicy="no-referrer"
          onError={() => setThumbFalhou(true)}
          className="size-full object-cover opacity-90 transition group-hover:opacity-100"
        />
      )}
      <span className="absolute inset-0 flex items-center justify-center">
        <span className="flex size-12 items-center justify-center rounded-full bg-black/60 text-white transition group-hover:bg-marca">
          <Play className="size-5 fill-current" />
        </span>
      </span>
      <span className="absolute bottom-2 left-2 rounded bg-black/60 px-1.5 py-0.5 text-[11px] font-medium text-white">
        {nomePlataforma(v)}
      </span>
    </button>
  );
}

/** Linha com os dados do vídeo: título, autor ou duração. */
export function LegendaVideo({ video: v }: { video: Video }) {
  const detalhes = [
    v.tipo === "embed" ? v.autor : null,
    v.duracao_s != null ? duracao(v.duracao_s) : null,
    !v.meu ? "do mentor" : null,
  ].filter(Boolean);
  return (
    <div className="min-w-0">
      <p className="line-clamp-2 text-sm font-medium leading-snug">{v.titulo || "Sem título"}</p>
      {detalhes.length > 0 && <p className="truncate text-xs text-suave">{detalhes.join(" · ")}</p>}
    </div>
  );
}
