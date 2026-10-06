import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap, type Schemas } from "@/api/client";

export const periods = [7, 30, 90] as const;
export type Days = (typeof periods)[number];

export type ResultsSearch = { days?: Days };

export function validateResultsSearch(s: Record<string, unknown>): ResultsSearch {
  const days = Number(s.days);
  return { days: periods.includes(days as Days) && days !== 30 ? (days as Days) : undefined };
}

/** Local date as YYYY-MM-DD. */
export function isoDate(d: Date): string {
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

/** The last `days` days up to today, inclusive. The backend keeps up to 89 days of synced history. */
export function period(days: number, today: Date = new Date()): { from: string; to: string } {
  const from = new Date(today.getFullYear(), today.getMonth(), today.getDate() - (days - 1));
  return { from: isoDate(from), to: isoDate(today) };
}

/** Fills the series with the days without sales, so the chart does not skip dates. */
export function fillDays(from: string, to: string, days: Schemas["DayResult"][]): Schemas["DayResult"][] {
  const byDay = new Map(days.map((d) => [d.day, d]));
  const out: Schemas["DayResult"][] = [];
  const [y = 0, m = 1, d = 1] = from.split("-").map(Number);
  for (let day = new Date(y, m - 1, d); isoDate(day) <= to; day.setDate(day.getDate() + 1)) {
    const k = isoDate(day);
    out.push(byDay.get(k) ?? { day: k, orders: 0, estimated_commission_cents: 0, validated_commission_cents: 0 });
  }
  return out;
}

export function useMyResults(workspaceId: string, days: number) {
  const p = period(days);
  return useQuery({
    queryKey: ["results", workspaceId, p.from, p.to],
    queryFn: () => unwrap(api.GET("/v1/workspaces/{workspaceId}/results", { params: { path: { workspaceId }, query: p } })),
  });
}

export function useGroupResults(workspaceId: string, days: number) {
  const p = period(days);
  return useQuery({
    queryKey: ["group-results", workspaceId, p.from, p.to],
    queryFn: () =>
      unwrap(api.GET("/v1/workspaces/{workspaceId}/results/group", { params: { path: { workspaceId }, query: p } })),
  });
}

/** The sync status; asks again every 3 s while it runs. */
export function useSync() {
  const qc = useQueryClient();
  return useQuery({
    queryKey: ["sync"],
    queryFn: async () => {
      const s = await unwrap(api.GET("/v1/me/results/sync"));
      const previous = qc.getQueryData<Schemas["ConversionSync"]>(["sync"]);
      if (previous?.status === "syncing" && s.status !== "syncing") {
        void qc.invalidateQueries({ queryKey: ["results"] });
        void qc.invalidateQueries({ queryKey: ["group-results"] });
      }
      return s;
    },
    refetchInterval: (q) => (q.state.data?.status === "syncing" ? 3000 : false),
  });
}

export function useRequestSync() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => unwrap(api.POST("/v1/me/results/sync")),
    onSuccess: (s) => qc.setQueryData(["sync"], s),
  });
}

export function useConsent(workspaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (sharesResults: boolean) =>
      unwrap(
        api.PUT("/v1/workspaces/{workspaceId}/results/consent", {
          params: { path: { workspaceId } },
          body: { shares_results: sharesResults },
        }),
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["results", workspaceId] });
      void qc.invalidateQueries({ queryKey: ["members", workspaceId] });
    },
  });
}
