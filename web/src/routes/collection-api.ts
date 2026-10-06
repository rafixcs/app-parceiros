import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type Item } from "@/api/client";

/** IDs of the products already saved in the workspace, to mark them on the radar. */
export function useSavedProducts(workspaceId: string) {
  return useQuery({
    queryKey: ["saved-products", workspaceId],
    queryFn: () =>
      unwrap(api.GET("/v1/workspaces/{workspaceId}/items/products", { params: { path: { workspaceId } } })),
    select: (ids) => new Set(ids),
  });
}

export function useCollections(workspaceId: string) {
  return useQuery({
    queryKey: ["collections", workspaceId],
    queryFn: () => unwrap(api.GET("/v1/workspaces/{workspaceId}/collections", { params: { path: { workspaceId } } })),
  });
}

/** Invalidates what depends on the collection after a change. */
export function useInvalidateCollection(workspaceId: string) {
  const qc = useQueryClient();
  return (item?: Item) => {
    if (item) qc.setQueryData(["item", workspaceId, item.id], item);
    void qc.invalidateQueries({ queryKey: ["items", workspaceId] });
    void qc.invalidateQueries({ queryKey: ["saved-products", workspaceId] });
    void qc.invalidateQueries({ queryKey: ["collections", workspaceId] });
  };
}

/** Saves a product from the radar or pasted by link. */
export function useSaveItem(workspaceId: string) {
  const invalidate = useInvalidateCollection(workspaceId);
  return useMutation({
    mutationFn: (body: { product_id: string } | { url: string }) =>
      unwrap(api.POST("/v1/workspaces/{workspaceId}/items", { params: { path: { workspaceId } }, body })),
    onSuccess: (item) => invalidate(item),
  });
}
