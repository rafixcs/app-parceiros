import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, exigir, type ListaDetalhe, type Video } from "@/api/cliente";

/** Enquanto algum vídeo está sendo processado, a consulta se atualiza sozinha. */
function processando(vs: Video[] | undefined) {
  return vs?.some((v) => v.status === "processando") ? 3000 : false;
}

/** Vídeos que o usuário vê no workspace; com produtoId, só os do produto. */
export function useVideos(workspaceId: string, produtoId?: string) {
  return useQuery({
    queryKey: ["videos", workspaceId, produtoId ?? "todos"],
    queryFn: () =>
      exigir(
        api.GET("/v1/workspaces/{workspaceId}/videos", {
          params: { path: { workspaceId }, query: produtoId ? { produto_id: produtoId } : {} },
        }),
      ),
    refetchInterval: (q) => processando(q.state.data),
  });
}

export function useCotaVideos(workspaceId: string) {
  return useQuery({
    queryKey: ["videos-cota", workspaceId],
    queryFn: () => exigir(api.GET("/v1/workspaces/{workspaceId}/videos/cota", { params: { path: { workspaceId } } })),
  });
}

/** Invalida tudo o que mostra vídeos: biblioteca, produtos, listas e cota. */
export function useInvalidarVideos(workspaceId: string) {
  const qc = useQueryClient();
  return () => {
    void qc.invalidateQueries({ queryKey: ["videos", workspaceId] });
    void qc.invalidateQueries({ queryKey: ["videos-cota", workspaceId] });
    void qc.invalidateQueries({ queryKey: ["lista", workspaceId] });
  };
}

export function useColarVideo(workspaceId: string, produtoId?: string) {
  const invalidar = useInvalidarVideos(workspaceId);
  return useMutation({
    mutationFn: (url: string) =>
      exigir(
        api.POST("/v1/workspaces/{workspaceId}/videos/embed", {
          params: { path: { workspaceId } },
          body: produtoId ? { url, produto_id: produtoId } : { url },
        }),
      ),
    onSuccess: invalidar,
  });
}

export function useEditarVideo(workspaceId: string) {
  const invalidar = useInvalidarVideos(workspaceId);
  return useMutation({
    mutationFn: ({ id, ...body }: { id: string; titulo?: string; compartilhado?: boolean }) =>
      exigir(
        api.PATCH("/v1/workspaces/{workspaceId}/videos/{videoId}", {
          params: { path: { workspaceId, videoId: id } },
          body,
        }),
      ),
    onSuccess: invalidar,
  });
}

export function useApagarVideo(workspaceId: string) {
  const invalidar = useInvalidarVideos(workspaceId);
  return useMutation({
    mutationFn: (id: string) =>
      exigir(api.DELETE("/v1/workspaces/{workspaceId}/videos/{videoId}", { params: { path: { workspaceId, videoId: id } } })),
    onSuccess: invalidar,
  });
}

/** Liga (ou tira) um vídeo do usuário a um produto. */
export function useVinculoProduto(workspaceId: string) {
  const invalidar = useInvalidarVideos(workspaceId);
  return useMutation({
    mutationFn: ({ id, produtoId, ligar }: { id: string; produtoId: string; ligar: boolean }) => {
      const opts = { params: { path: { workspaceId, videoId: id, produtoId } } };
      return exigir(
        ligar
          ? api.PUT("/v1/workspaces/{workspaceId}/videos/{videoId}/produtos/{produtoId}", opts)
          : api.DELETE("/v1/workspaces/{workspaceId}/videos/{videoId}/produtos/{produtoId}", opts),
      );
    },
    onSuccess: invalidar,
  });
}

/** Anexa (ou tira) um vídeo de uma lista da curadoria. */
export function useVideoLista(workspaceId: string, listaId: string) {
  const qc = useQueryClient();
  const invalidar = useInvalidarVideos(workspaceId);
  return useMutation({
    mutationFn: ({ id, anexar }: { id: string; anexar: boolean }) => {
      const opts = { params: { path: { workspaceId, listaId, videoId: id } } };
      return exigir(
        anexar
          ? api.PUT("/v1/workspaces/{workspaceId}/listas/{listaId}/videos/{videoId}", opts)
          : api.DELETE("/v1/workspaces/{workspaceId}/listas/{listaId}/videos/{videoId}", opts),
      );
    },
    onSuccess: (l: ListaDetalhe) => {
      qc.setQueryData(["lista", workspaceId, l.id], l);
      invalidar();
    },
  });
}

/** Baixa o arquivo original por uma URL assinada de curta duração. */
export async function baixarVideo(workspaceId: string, id: string) {
  const { url } = await exigir(
    api.GET("/v1/workspaces/{workspaceId}/videos/{videoId}/download", { params: { path: { workspaceId, videoId: id } } }),
  );
  // A resposta pede ao navegador para salvar o arquivo: a página não muda.
  window.location.assign(url);
}
