import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type CuratedListDetail } from "@/api/client";

export function useLists(workspaceId: string) {
  return useQuery({
    queryKey: ["lists", workspaceId],
    queryFn: () => unwrap(api.GET("/v1/workspaces/{workspaceId}/lists", { params: { path: { workspaceId } } })),
  });
}

export function useList(workspaceId: string, listId: string) {
  return useQuery({
    queryKey: ["list", workspaceId, listId],
    queryFn: () =>
      unwrap(api.GET("/v1/workspaces/{workspaceId}/lists/{listId}", { params: { path: { workspaceId, listId } } })),
    // While some imported link is being generated, the list refreshes by itself.
    refetchInterval: (q) => (q.state.data?.items.some((i) => i.my_item?.link_status === "generating") ? 2500 : false),
  });
}

/** Stores the list a change returned and invalidates what depends on it. */
export function useUpdateList(workspaceId: string) {
  const qc = useQueryClient();
  return (l?: CuratedListDetail) => {
    if (l) qc.setQueryData(["list", workspaceId, l.id], l);
    void qc.invalidateQueries({ queryKey: ["lists", workspaceId] });
  };
}

export function useCreateList(workspaceId: string) {
  const update = useUpdateList(workspaceId);
  return useMutation({
    mutationFn: (body: { title: string; description?: string }) =>
      unwrap(api.POST("/v1/workspaces/{workspaceId}/lists", { params: { path: { workspaceId } }, body })),
    onSuccess: (l) => update(l),
  });
}
