import { Link } from "@tanstack/react-router";
import { Clapperboard, Plus, Unlink } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Select } from "@/components/ui/input";
import { AdicionarVideo } from "@/components/video/adicionar";
import { useWorkspaceAtual } from "./listas-api";
import { CartaoVideo } from "./videos";
import { useVideos, useVinculoProduto } from "./videos-api";

/**
 * Vídeos ligados ao produto: as referências e os vídeos próprios de quem vê
 * e os que o mentor compartilhou. Dá para adicionar um novo ou ligar um da
 * biblioteca.
 */
export function VideosDoProduto({ workspaceId, produtoId }: { workspaceId: string; produtoId: string }) {
  const { gestor } = useWorkspaceAtual(workspaceId);
  const doProduto = useVideos(workspaceId, produtoId);
  const biblioteca = useVideos(workspaceId);
  const vinculo = useVinculoProduto(workspaceId);
  const [adicionando, setAdicionando] = useState(false);
  const [escolhido, setEscolhido] = useState("");

  const ligados = new Set(doProduto.data?.map((v) => v.id));
  const livres = (biblioteca.data ?? []).filter((v) => v.meu && !ligados.has(v.id));

  return (
    <Card className="flex flex-col gap-3 p-4">
      <div className="flex items-center gap-2">
        <Clapperboard className="size-4 text-suave" />
        <h2 className="mr-auto font-medium">Vídeos</h2>
        <Button tamanho="sm" variante="secundario" onClick={() => setAdicionando((a) => !a)}>
          <Plus className="size-4" /> Adicionar
        </Button>
      </div>

      {adicionando && (
        <div className="flex flex-col gap-3 rounded-lg border border-borda p-3">
          <AdicionarVideo workspaceId={workspaceId} produtoId={produtoId} onAdicionado={() => setAdicionando(false)} />
          {livres.length > 0 && (
            <form
              className="flex gap-2 border-t border-borda pt-3"
              onSubmit={(e) => {
                e.preventDefault();
                vinculo.mutate(
                  { id: escolhido, produtoId, ligar: true },
                  {
                    onSuccess: () => {
                      setEscolhido("");
                      setAdicionando(false);
                    },
                  },
                );
              }}
            >
              <Select aria-label="Vídeo da biblioteca" value={escolhido} onChange={(e) => setEscolhido(e.target.value)}>
                <option value="">Ou escolha da sua biblioteca…</option>
                {livres.map((v) => (
                  <option key={v.id} value={v.id}>
                    {v.titulo || "Sem título"}
                  </option>
                ))}
              </Select>
              <Button type="submit" variante="secundario" disabled={!escolhido || vinculo.isPending}>
                Ligar
              </Button>
            </form>
          )}
          {vinculo.error && <p className="text-sm text-red-700">{vinculo.error.message}</p>}
        </div>
      )}

      {doProduto.data?.length === 0 && !adicionando && (
        <p className="text-sm text-suave">
          Guarde aqui vídeos de referência do TikTok e do YouTube e os seus vídeos deste produto.{" "}
          <Link to="/w/$workspaceId/videos" params={{ workspaceId }} className="text-marca hover:underline">
            Ver todos os vídeos
          </Link>
        </p>
      )}
      {doProduto.error && <p className="text-sm text-red-700">{doProduto.error.message}</p>}
      <ul className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        {doProduto.data?.map((v) => (
          <li key={v.id}>
            <CartaoVideo
              video={v}
              workspaceId={workspaceId}
              podeCompartilhar={gestor}
              acoes={
                v.meu && (
                  <Button
                    variante="fantasma"
                    tamanho="sm"
                    title="Tirar deste produto"
                    disabled={vinculo.isPending}
                    onClick={() => vinculo.mutate({ id: v.id, produtoId, ligar: false })}
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
