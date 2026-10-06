import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "@/api/client";

export type Subscription = Schemas["Subscription"];

/** Invalidates what depends on the workspace's access after a billing change. */
function useRefresh(workspaceId: string) {
  const qc = useQueryClient();
  return (s?: Subscription) => {
    if (s) qc.setQueryData(["subscription", workspaceId], s);
    void qc.invalidateQueries({ queryKey: ["workspaces"] });
  };
}

export function useSubscription(workspaceId: string) {
  return useQuery({
    queryKey: ["subscription", workspaceId],
    queryFn: () =>
      unwrap(api.GET("/v1/workspaces/{workspaceId}/subscription", { params: { path: { workspaceId } } })),
    // While the payment has not arrived, the page refreshes by itself: the
    // gateway notifies the API through the webhook, not the browser.
    refetchInterval: (q) => (q.state.data?.status === "pending" ? 10000 : false),
  });
}

export function useSubscribe(workspaceId: string) {
  const refresh = useRefresh(workspaceId);
  return useMutation({
    mutationFn: (body: { seats?: number; tax_id: string }) =>
      unwrap(api.POST("/v1/workspaces/{workspaceId}/subscription", { params: { path: { workspaceId } }, body })),
    onSuccess: refresh,
  });
}

export function useChangeSeats(workspaceId: string) {
  const refresh = useRefresh(workspaceId);
  return useMutation({
    mutationFn: (seats: number) =>
      unwrap(
        api.PATCH("/v1/workspaces/{workspaceId}/subscription", {
          params: { path: { workspaceId } },
          body: { seats },
        }),
      ),
    onSuccess: refresh,
  });
}

export function useCancelSubscription(workspaceId: string) {
  const refresh = useRefresh(workspaceId);
  return useMutation({
    mutationFn: () =>
      unwrap(api.DELETE("/v1/workspaces/{workspaceId}/subscription", { params: { path: { workspaceId } } })),
    onSuccess: refresh,
  });
}

/** Local environment only, when the subscription comes with `simulated`. */
export function useSimulatePayment(workspaceId: string) {
  const refresh = useRefresh(workspaceId);
  return useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/v1/workspaces/{workspaceId}/subscription/simulate-payment", {
          params: { path: { workspaceId } },
        }),
      ),
    onSuccess: refresh,
  });
}

/** Date in the everyday format: "08/10/2026". */
export function shortDate(iso: string): string {
  return new Date(iso).toLocaleDateString("pt-BR", { day: "2-digit", month: "2-digit", year: "numeric" });
}

/** Whole days left until `iso`; negative when it has passed. */
export function daysUntil(iso: string, now: Date = new Date()): number {
  return Math.ceil((new Date(iso).getTime() - now.getTime()) / 86400000);
}

/** How the workspace's access status shows on screen. */
export function accessStatusLabel(s: Subscription): string {
  switch (s.access_status) {
    case "active":
      return "Assinatura ativa";
    case "suspended":
      return "Workspace suspenso";
    case "free":
      return "Grátis para alunos de mentoria";
    default: {
      const d = daysUntil(s.access_until);
      return d <= 1 ? "Teste terminando" : `Teste · ${d} dias restantes`;
    }
  }
}
