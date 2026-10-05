import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, exigir, type Schemas } from "@/api/cliente";

export type Assinatura = Schemas["Assinatura"];

/** Invalida o que depende do acesso do workspace depois de uma cobrança. */
function useAtualizar(workspaceId: string) {
  const qc = useQueryClient();
  return (a?: Assinatura) => {
    if (a) qc.setQueryData(["assinatura", workspaceId], a);
    void qc.invalidateQueries({ queryKey: ["workspaces"] });
  };
}

export function useAssinatura(workspaceId: string) {
  return useQuery({
    queryKey: ["assinatura", workspaceId],
    queryFn: () => exigir(api.GET("/v1/workspaces/{workspaceId}/assinatura", { params: { path: { workspaceId } } })),
    // Enquanto o pagamento não cai, a tela se atualiza sozinha: o gateway
    // avisa a API pelo webhook, não o navegador.
    refetchInterval: (q) => (q.state.data?.status === "aguardando" ? 10000 : false),
  });
}

export function useAssinar(workspaceId: string) {
  const atualizar = useAtualizar(workspaceId);
  return useMutation({
    mutationFn: (corpo: { assentos?: number; cpf_cnpj: string }) =>
      exigir(api.POST("/v1/workspaces/{workspaceId}/assinatura", { params: { path: { workspaceId } }, body: corpo })),
    onSuccess: atualizar,
  });
}

export function useMudarAssentos(workspaceId: string) {
  const atualizar = useAtualizar(workspaceId);
  return useMutation({
    mutationFn: (assentos: number) =>
      exigir(
        api.PATCH("/v1/workspaces/{workspaceId}/assinatura", {
          params: { path: { workspaceId } },
          body: { assentos },
        }),
      ),
    onSuccess: atualizar,
  });
}

export function useCancelarAssinatura(workspaceId: string) {
  const atualizar = useAtualizar(workspaceId);
  return useMutation({
    mutationFn: () =>
      exigir(api.DELETE("/v1/workspaces/{workspaceId}/assinatura", { params: { path: { workspaceId } } })),
    onSuccess: atualizar,
  });
}

/** Só no ambiente local, quando a assinatura vem com `simulavel`. */
export function useSimularPagamento(workspaceId: string) {
  const atualizar = useAtualizar(workspaceId);
  return useMutation({
    mutationFn: () =>
      exigir(
        api.POST("/v1/workspaces/{workspaceId}/assinatura/simular-pagamento", {
          params: { path: { workspaceId } },
        }),
      ),
    onSuccess: atualizar,
  });
}

/** Data no formato do dia a dia: "08/10/2026". */
export function dia(iso: string): string {
  return new Date(iso).toLocaleDateString("pt-BR", { day: "2-digit", month: "2-digit", year: "numeric" });
}

/** Dias inteiros que faltam até `iso`; negativo já passou. */
export function diasAte(iso: string, agora: Date = new Date()): number {
  return Math.ceil((new Date(iso).getTime() - agora.getTime()) / 86400000);
}

/** Como a situação do workspace aparece na tela. */
export function rotuloSituacao(a: Assinatura): string {
  switch (a.situacao) {
    case "ativo":
      return "Assinatura ativa";
    case "suspenso":
      return "Workspace suspenso";
    default: {
      const d = diasAte(a.acesso_ate);
      return d <= 1 ? "Teste terminando" : `Teste · ${d} dias restantes`;
    }
  }
}
