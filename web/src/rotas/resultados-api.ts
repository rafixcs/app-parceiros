import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, exigir, type Schemas } from "@/api/cliente";

export const periodos = [7, 30, 90] as const;
export type Dias = (typeof periodos)[number];

export type BuscaResultados = { dias?: Dias };

export function validarBuscaResultados(s: Record<string, unknown>): BuscaResultados {
  const dias = Number(s.dias);
  return { dias: periodos.includes(dias as Dias) && dias !== 30 ? (dias as Dias) : undefined };
}

/** Data local no formato AAAA-MM-DD. */
export function dataISO(d: Date): string {
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

/** Os últimos `dias` dias até hoje, inclusive. O backend aceita até 89 dias de histórico sincronizado. */
export function periodo(dias: number, hoje: Date = new Date()): { de: string; ate: string } {
  const de = new Date(hoje.getFullYear(), hoje.getMonth(), hoje.getDate() - (dias - 1));
  return { de: dataISO(de), ate: dataISO(hoje) };
}

/** Completa a série com os dias sem venda, para o gráfico não pular datas. */
export function preencherDias(de: string, ate: string, dias: Schemas["ResultadoDia"][]): Schemas["ResultadoDia"][] {
  const porDia = new Map(dias.map((d) => [d.dia, d]));
  const out: Schemas["ResultadoDia"][] = [];
  const [a = 0, m = 1, d = 1] = de.split("-").map(Number);
  for (let dia = new Date(a, m - 1, d); dataISO(dia) <= ate; dia.setDate(dia.getDate() + 1)) {
    const k = dataISO(dia);
    out.push(porDia.get(k) ?? { dia: k, pedidos: 0, comissao_estimada_centavos: 0, comissao_validada_centavos: 0 });
  }
  return out;
}

export function useMeusResultados(workspaceId: string, dias: number) {
  const p = periodo(dias);
  return useQuery({
    queryKey: ["resultados", workspaceId, p.de, p.ate],
    queryFn: () =>
      exigir(api.GET("/v1/workspaces/{workspaceId}/resultados", { params: { path: { workspaceId }, query: p } })),
  });
}

export function useResultadosTurma(workspaceId: string, dias: number) {
  const p = periodo(dias);
  return useQuery({
    queryKey: ["resultados-turma", workspaceId, p.de, p.ate],
    queryFn: () =>
      exigir(api.GET("/v1/workspaces/{workspaceId}/resultados/turma", { params: { path: { workspaceId }, query: p } })),
  });
}

/** Situação da sincronização; consulta de novo a cada 3 s enquanto ela roda. */
export function useSincronizacao() {
  const qc = useQueryClient();
  return useQuery({
    queryKey: ["sincronizacao"],
    queryFn: async () => {
      const s = await exigir(api.GET("/v1/eu/resultados/sincronizacao"));
      const anterior = qc.getQueryData<Schemas["Sincronizacao"]>(["sincronizacao"]);
      if (anterior?.status === "sincronizando" && s.status !== "sincronizando") {
        void qc.invalidateQueries({ queryKey: ["resultados"] });
        void qc.invalidateQueries({ queryKey: ["resultados-turma"] });
      }
      return s;
    },
    refetchInterval: (q) => (q.state.data?.status === "sincronizando" ? 3000 : false),
  });
}

export function useSincronizar() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => exigir(api.POST("/v1/eu/resultados/sincronizar")),
    onSuccess: (s) => qc.setQueryData(["sincronizacao"], s),
  });
}

export function useConsentimento(workspaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (consente: boolean) =>
      exigir(
        api.PUT("/v1/workspaces/{workspaceId}/resultados/consentimento", {
          params: { path: { workspaceId } },
          body: { consente },
        }),
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["resultados", workspaceId] });
      void qc.invalidateQueries({ queryKey: ["membros", workspaceId] });
    },
  });
}
