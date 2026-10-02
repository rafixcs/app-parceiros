import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, exigir, type ListaDetalhe } from "@/api/cliente";
import { useWorkspaces } from "./layout";

/** O workspace aberto e se quem vê pode montar listas e gerir a turma. */
export function useWorkspaceAtual(workspaceId: string) {
  const ws = useWorkspaces();
  const atual = ws.data?.find((w) => w.id === workspaceId);
  return {
    workspace: atual,
    carregando: ws.isPending,
    gestor: atual?.tipo === "mentoria" && (atual.papel === "dono" || atual.papel === "mentor"),
  };
}

export function useListas(workspaceId: string) {
  return useQuery({
    queryKey: ["listas", workspaceId],
    queryFn: () => exigir(api.GET("/v1/workspaces/{workspaceId}/listas", { params: { path: { workspaceId } } })),
  });
}

export function useLista(workspaceId: string, listaId: string) {
  return useQuery({
    queryKey: ["lista", workspaceId, listaId],
    queryFn: () =>
      exigir(
        api.GET("/v1/workspaces/{workspaceId}/listas/{listaId}", { params: { path: { workspaceId, listaId } } }),
      ),
    // Enquanto algum link importado está sendo gerado, a lista se atualiza sozinha.
    refetchInterval: (q) => (q.state.data?.itens.some((i) => i.meu_item?.link_status === "gerando") ? 2500 : false),
  });
}

/** Grava a lista devolvida por uma mudança e invalida o que depende dela. */
export function useAtualizarLista(workspaceId: string) {
  const qc = useQueryClient();
  return (l?: ListaDetalhe) => {
    if (l) qc.setQueryData(["lista", workspaceId, l.id], l);
    void qc.invalidateQueries({ queryKey: ["listas", workspaceId] });
  };
}

export function useCriarLista(workspaceId: string) {
  const atualizar = useAtualizarLista(workspaceId);
  return useMutation({
    mutationFn: (corpo: { titulo: string; descricao?: string }) =>
      exigir(api.POST("/v1/workspaces/{workspaceId}/listas", { params: { path: { workspaceId } }, body: corpo })),
    onSuccess: (l) => atualizar(l),
  });
}

export function useNaoLidas(workspaceId: string | undefined) {
  return useQuery({
    queryKey: ["notificacoes", workspaceId],
    enabled: !!workspaceId,
    queryFn: () =>
      exigir(
        api.GET("/v1/workspaces/{workspaceId}/notificacoes", { params: { path: { workspaceId: workspaceId! } } }),
      ),
    refetchInterval: 60_000,
    refetchOnWindowFocus: true,
  });
}
