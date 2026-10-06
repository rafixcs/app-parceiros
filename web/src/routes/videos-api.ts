import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type CuratedListDetail, type Video } from "@/api/client";

/** While some video is processing, the query refreshes by itself. */
function processing(vs: Video[] | undefined) {
  return vs?.some((v) => v.status === "processing") ? 3000 : false;
}

/** Videos the user sees in the workspace; with productId, only the product's. */
export function useVideos(workspaceId: string, productId?: string) {
  return useQuery({
    queryKey: ["videos", workspaceId, productId ?? "all"],
    queryFn: () =>
      unwrap(
        api.GET("/v1/workspaces/{workspaceId}/videos", {
          params: { path: { workspaceId }, query: productId ? { product_id: productId } : {} },
        }),
      ),
    refetchInterval: (q) => processing(q.state.data),
  });
}

export function useVideoQuota(workspaceId: string) {
  return useQuery({
    queryKey: ["videos-quota", workspaceId],
    queryFn: () => unwrap(api.GET("/v1/workspaces/{workspaceId}/videos/quota", { params: { path: { workspaceId } } })),
  });
}

/** Invalidates everything that shows videos: library, products, lists and quota. */
export function useInvalidateVideos(workspaceId: string) {
  const qc = useQueryClient();
  return () => {
    void qc.invalidateQueries({ queryKey: ["videos", workspaceId] });
    void qc.invalidateQueries({ queryKey: ["videos-quota", workspaceId] });
    void qc.invalidateQueries({ queryKey: ["list", workspaceId] });
  };
}

export function usePasteVideo(workspaceId: string, productId?: string) {
  const invalidate = useInvalidateVideos(workspaceId);
  return useMutation({
    mutationFn: (url: string) =>
      unwrap(
        api.POST("/v1/workspaces/{workspaceId}/videos/embed", {
          params: { path: { workspaceId } },
          body: productId ? { url, product_id: productId } : { url },
        }),
      ),
    onSuccess: invalidate,
  });
}

export function useUpdateVideo(workspaceId: string) {
  const invalidate = useInvalidateVideos(workspaceId);
  return useMutation({
    mutationFn: ({ id, ...body }: { id: string; title?: string; shared?: boolean }) =>
      unwrap(
        api.PATCH("/v1/workspaces/{workspaceId}/videos/{videoId}", {
          params: { path: { workspaceId, videoId: id } },
          body,
        }),
      ),
    onSuccess: invalidate,
  });
}

export function useDeleteVideo(workspaceId: string) {
  const invalidate = useInvalidateVideos(workspaceId);
  return useMutation({
    mutationFn: (id: string) =>
      unwrap(api.DELETE("/v1/workspaces/{workspaceId}/videos/{videoId}", { params: { path: { workspaceId, videoId: id } } })),
    onSuccess: invalidate,
  });
}

/** Links (or unlinks) one of the user's videos to a product. */
export function useProductLink(workspaceId: string) {
  const invalidate = useInvalidateVideos(workspaceId);
  return useMutation({
    mutationFn: ({ id, productId, linked }: { id: string; productId: string; linked: boolean }) => {
      const opts = { params: { path: { workspaceId, videoId: id, productId } } };
      return unwrap(
        linked
          ? api.PUT("/v1/workspaces/{workspaceId}/videos/{videoId}/products/{productId}", opts)
          : api.DELETE("/v1/workspaces/{workspaceId}/videos/{videoId}/products/{productId}", opts),
      );
    },
    onSuccess: invalidate,
  });
}

/** Attaches (or detaches) a video to a curated list. */
export function useListVideo(workspaceId: string, listId: string) {
  const qc = useQueryClient();
  const invalidate = useInvalidateVideos(workspaceId);
  return useMutation({
    mutationFn: ({ id, attached }: { id: string; attached: boolean }) => {
      const opts = { params: { path: { workspaceId, listId, videoId: id } } };
      return unwrap(
        attached
          ? api.PUT("/v1/workspaces/{workspaceId}/lists/{listId}/videos/{videoId}", opts)
          : api.DELETE("/v1/workspaces/{workspaceId}/lists/{listId}/videos/{videoId}", opts),
      );
    },
    onSuccess: (l: CuratedListDetail) => {
      qc.setQueryData(["list", workspaceId, l.id], l);
      invalidate();
    },
  });
}

/** Downloads the original file through a short-lived signed URL. */
export async function downloadVideo(workspaceId: string, id: string) {
  const { url } = await unwrap(
    api.GET("/v1/workspaces/{workspaceId}/videos/{videoId}/download", { params: { path: { workspaceId, videoId: id } } }),
  );
  // The response asks the browser to save the file: the page does not change.
  window.location.assign(url);
}
