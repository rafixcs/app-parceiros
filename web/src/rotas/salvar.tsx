import { Link } from "@tanstack/react-router";
import { Bookmark, BookmarkCheck } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useProdutosSalvos, useSalvar } from "./colecao-api";

/** Botão "Salvar" do radar. Depois de salvo, leva à coleção. */
export function BotaoSalvar({
  workspaceId,
  produtoId,
  className,
}: {
  workspaceId: string;
  produtoId: string;
  className?: string;
}) {
  const salvos = useProdutosSalvos(workspaceId);
  const salvar = useSalvar(workspaceId);
  const salvo = salvos.data?.has(produtoId) || salvar.isSuccess;

  if (salvo) {
    return (
      <Link
        to="/w/$workspaceId/colecao"
        params={{ workspaceId }}
        search={{}}
        className={cn(
          "inline-flex h-8 items-center gap-1.5 rounded-lg bg-emerald-50 px-3 text-sm font-medium text-emerald-800 hover:bg-emerald-100",
          className,
        )}
        title="Ver na sua coleção"
      >
        <BookmarkCheck className="size-4" /> Salvo
      </Link>
    );
  }
  return (
    <Button
      tamanho="sm"
      variante="secundario"
      className={className}
      disabled={salvar.isPending}
      title={salvar.error?.message ?? "Salvar na sua coleção e gerar o seu link"}
      onClick={() => salvar.mutate({ produto_id: produtoId })}
    >
      <Bookmark className="size-4" /> {salvar.isPending ? "Salvando…" : "Salvar"}
    </Button>
  );
}
