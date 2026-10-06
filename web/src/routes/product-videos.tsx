import { Link } from "@tanstack/react-router";
import { Clapperboard, Plus, Unlink } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Select } from "@/components/ui/input";
import { AddVideo } from "@/components/video/add";
import { useCurrentWorkspace } from "./layout";
import { VideoCard } from "./videos";
import { useProductLink, useVideos } from "./videos-api";

/**
 * Videos linked to the product: the viewer's references and own videos and
 * the ones the mentor shared. A new one can be added or one linked from the
 * library.
 */
export function ProductVideos({ workspaceId, productId }: { workspaceId: string; productId: string }) {
  const { manager } = useCurrentWorkspace(workspaceId);
  const ofProduct = useVideos(workspaceId, productId);
  const library = useVideos(workspaceId);
  const link = useProductLink(workspaceId);
  const [adding, setAdding] = useState(false);
  const [chosen, setChosen] = useState("");

  const linked = new Set(ofProduct.data?.map((v) => v.id));
  const available = (library.data ?? []).filter((v) => v.mine && !linked.has(v.id));

  return (
    <Card className="flex flex-col gap-3 p-4">
      <div className="flex items-center gap-2">
        <Clapperboard className="size-4 text-muted" />
        <h2 className="mr-auto font-medium">Vídeos</h2>
        <Button size="sm" variant="secondary" onClick={() => setAdding((a) => !a)}>
          <Plus className="size-4" /> Adicionar
        </Button>
      </div>

      {adding && (
        <div className="flex flex-col gap-3 rounded-lg border border-border p-3">
          <AddVideo workspaceId={workspaceId} productId={productId} onAdded={() => setAdding(false)} />
          {available.length > 0 && (
            <form
              className="flex gap-2 border-t border-border pt-3"
              onSubmit={(e) => {
                e.preventDefault();
                link.mutate(
                  { id: chosen, productId, linked: true },
                  {
                    onSuccess: () => {
                      setChosen("");
                      setAdding(false);
                    },
                  },
                );
              }}
            >
              <Select aria-label="Vídeo da biblioteca" value={chosen} onChange={(e) => setChosen(e.target.value)}>
                <option value="">Ou escolha da sua biblioteca…</option>
                {available.map((v) => (
                  <option key={v.id} value={v.id}>
                    {v.title || "Sem título"}
                  </option>
                ))}
              </Select>
              <Button type="submit" variant="secondary" disabled={!chosen || link.isPending}>
                Ligar
              </Button>
            </form>
          )}
          {link.error && <p className="text-sm text-red-700">{link.error.message}</p>}
        </div>
      )}

      {ofProduct.data?.length === 0 && !adding && (
        <p className="text-sm text-muted">
          Guarde aqui vídeos de referência do TikTok e do YouTube e os seus vídeos deste produto.{" "}
          <Link to="/w/$workspaceId/videos" params={{ workspaceId }} className="text-brand hover:underline">
            Ver todos os vídeos
          </Link>
        </p>
      )}
      {ofProduct.error && <p className="text-sm text-red-700">{ofProduct.error.message}</p>}
      <ul className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        {ofProduct.data?.map((v) => (
          <li key={v.id}>
            <VideoCard
              video={v}
              workspaceId={workspaceId}
              canShare={manager}
              actions={
                v.mine && (
                  <Button
                    variant="ghost"
                    size="sm"
                    title="Tirar deste produto"
                    disabled={link.isPending}
                    onClick={() => link.mutate({ id: v.id, productId, linked: false })}
                  >
                    <Unlink className="size-4" />
                    <span className="sr-only">Tirar deste produto</span>
                  </Button>
                )
              }
            />
          </li>
        ))}
      </ul>
    </Card>
  );
}
