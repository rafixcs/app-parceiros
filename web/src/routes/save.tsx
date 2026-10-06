import { Link } from "@tanstack/react-router";
import { Bookmark, BookmarkCheck } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useSaveItem, useSavedProducts } from "./collection-api";

/** The radar's "Salvar" button. Once saved, it leads to the collection. */
export function SaveButton({
  workspaceId,
  productId,
  className,
}: {
  workspaceId: string;
  productId: string;
  className?: string;
}) {
  const saved = useSavedProducts(workspaceId);
  const save = useSaveItem(workspaceId);
  const isSaved = saved.data?.has(productId) || save.isSuccess;

  if (isSaved) {
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
      size="sm"
      variant="secondary"
      className={className}
      disabled={save.isPending}
      title={save.error?.message ?? "Salvar na sua coleção e gerar o seu link"}
      onClick={() => save.mutate({ product_id: productId })}
    >
      <Bookmark className="size-4" /> {save.isPending ? "Salvando…" : "Salvar"}
    </Button>
  );
}
