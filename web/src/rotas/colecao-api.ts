import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, exigir, type Item } from "@/api/cliente";

/** IDs dos produtos já salvos no workspace, para marcar o radar. */
export function useProdutosSalvos(workspaceId: string) {
  return useQuery({
    queryKey: ["produtos-salvos", workspaceId],
    queryFn: () =>
      exigir(api.GET("/v1/workspaces/{workspaceId}/itens/produtos", { params: { path: { workspaceId } } })),
    select: (ids) => new Set(ids),
  });
}

export function useColecoes(workspaceId: string) {
  return useQuery({
    queryKey: ["colecoes", workspaceId],
    queryFn: () => exigir(api.GET("/v1/workspaces/{workspaceId}/colecoes", { params: { path: { workspaceId } } })),
  });
}

/** Invalida o que depende da coleção depois de uma mudança. */
export function useInvalidarColecao(workspaceId: string) {
  const qc = useQueryClient();
  return (item?: Item) => {
    if (item) qc.setQueryData(["item", workspaceId, item.id], item);
    void qc.invalidateQueries({ queryKey: ["itens", workspaceId] });
    void qc.invalidateQueries({ queryKey: ["produtos-salvos", workspaceId] });
    void qc.invalidateQueries({ queryKey: ["colecoes", workspaceId] });
  };
}

/** Salva um produto do radar ou colado por link. */
export function useSalvar(workspaceId: string) {
  const invalidar = useInvalidarColecao(workspaceId);
  return useMutation({
    mutationFn: (corpo: { produto_id: string } | { url: string }) =>
      exigir(api.POST("/v1/workspaces/{workspaceId}/itens", { params: { path: { workspaceId } }, body: corpo })),
    onSuccess: (item) => invalidar(item),
  });
}
