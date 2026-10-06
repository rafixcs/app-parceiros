import { useQuery } from "@tanstack/react-query";
import { Link, Outlet, useNavigate, useParams } from "@tanstack/react-router";
import { BarChart3, Bell, Bookmark, Clapperboard, CreditCard, Flame, ListChecks, LogOut, Plug, Users } from "lucide-react";
import { useEffect } from "react";
import { api, unwrap, type Workspace } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/card";
import { Select } from "@/components/ui/input";
import { signOut } from "@/lib/auth";
import { cn } from "@/lib/utils";
import { EmailVerificationNotice } from "./internal-auth";
import { useInbox } from "./notifications";
import { daysUntil } from "./subscription-api";

const lastWorkspaceKey = "parceiros.workspace";

/**
 * Billing notice at the top of the app: the trial ending and the workspace suspended.
 * The affiliate pays nothing, so for them the notice only says whom to talk to.
 */
function BillingNotice({ ws }: { ws: Workspace }) {
  const days = daysUntil(ws.access_until);
  const owner = ws.role === "owner";
  const suspended = ws.status === "suspended";
  if (!suspended && days > 3) return null;
  return (
    <Notice
      className={cn(
        "flex flex-wrap items-center gap-2",
        suspended ? "border-red-200 bg-red-50 text-red-800" : "border-amber-200 bg-amber-50 text-amber-900",
      )}
    >
      <span>
        {suspended
          ? "Este workspace está suspenso por falta de pagamento."
          : days <= 0
            ? "O período de teste terminou."
            : `O período de teste termina em ${days === 1 ? "1 dia" : `${days} dias`}.`}
        {!owner && " Fale com o dono do workspace."}
      </span>
      {owner && (
        <Link
          to="/w/$workspaceId/assinatura"
          params={{ workspaceId: ws.id }}
          className="font-medium underline underline-offset-2"
        >
          Cuidar da assinatura
        </Link>
      )}
    </Notice>
  );
}

/** Last workspace opened, to go back to it outside the workspace routes. */
export function lastWorkspace(): string | null {
  try {
    return localStorage.getItem(lastWorkspaceKey);
  } catch {
    return null;
  }
}

export function useWorkspaces() {
  return useQuery({ queryKey: ["workspaces"], queryFn: () => unwrap(api.GET("/v1/workspaces")) });
}

/** The open workspace and whether the viewer can build lists and manage the cohort. */
export function useCurrentWorkspace(workspaceId: string) {
  const ws = useWorkspaces();
  const current = ws.data?.find((w) => w.id === workspaceId);
  return {
    workspace: current,
    loading: ws.isPending,
    manager: current?.kind === "mentorship" && (current.role === "owner" || current.role === "mentor"),
  };
}

export function useShopeeConnection() {
  return useQuery({ queryKey: ["shopee"], queryFn: () => unwrap(api.GET("/v1/me/shopee")) });
}

export function Layout() {
  const navigate = useNavigate();
  const { workspaceId } = useParams({ strict: false });
  const workspaces = useWorkspaces();
  const connection = useShopeeConnection();
  const me = useQuery({ queryKey: ["me"], queryFn: () => unwrap(api.GET("/v1/me")) });

  const connected = connection.data?.status === "connected";
  useEffect(() => {
    try {
      if (workspaceId) localStorage.setItem(lastWorkspaceKey, workspaceId);
    } catch {
      // Without localStorage, the selector just does not remember the choice.
    }
  }, [workspaceId]);
  const current = workspaceId ?? lastWorkspace() ?? "";
  const ws = workspaces.data?.find((w) => w.id === current);
  // In a mentorship, only the owner and mentors manage the cohort; in the personal one, the link leads to creating a mentorship.
  const showCohort = ws?.kind === "personal" || ws?.role === "owner" || ws?.role === "mentor";
  // The subscription belongs to the owner (the mentor follows it); the affiliate pays nothing.
  const showSubscription = ws?.role === "owner" || ws?.role === "mentor";
  const unread = useInbox(current || undefined).data?.unread ?? 0;
  const section = "flex h-9 shrink-0 items-center gap-2 rounded-lg px-3 text-sm hover:bg-zinc-100";

  return (
    <div className="min-h-dvh">
      <header className="sticky top-0 z-10 border-b border-border bg-white/90 backdrop-blur">
        <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2">
          <Link to="/" className="flex shrink-0 items-center gap-2 font-semibold">
            <img src="/icon.svg" alt="" className="size-7" />
            <span className="hidden 2xl:inline">Parceiros</span>
          </Link>
          {workspaces.data && workspaces.data.length > 0 && (
            <Select
              aria-label="Workspace"
              className="h-9 w-28 shrink-0 sm:w-44"
              value={current}
              onChange={(e) =>
                navigate({ to: "/w/$workspaceId/radar", params: { workspaceId: e.target.value }, search: {} })
              }
            >
              {!current && <option value="">Escolha um workspace</option>}
              {workspaces.data.map((w) => (
                <option key={w.id} value={w.id}>
                  {w.kind === "personal" ? "Pessoal" : w.name}
                </option>
              ))}
            </Select>
          )}
          {current && (
            <nav className="order-last flex w-full min-w-0 items-center gap-1 overflow-x-auto [scrollbar-width:none] sm:order-none sm:w-auto" aria-label="Seções">
              <Link
                to="/w/$workspaceId/radar"
                params={{ workspaceId: current }}
                search={{}}
                className={section}
                title="Radar"
                activeProps={{ className: "bg-zinc-100 font-medium" }}
              >
                <Flame className="size-4" />
                <span className="hidden xl:inline">Radar</span>
              </Link>
              <Link
                to="/w/$workspaceId/colecao"
                params={{ workspaceId: current }}
                search={{}}
                className={section}
                title="Coleção"
                activeProps={{ className: "bg-zinc-100 font-medium" }}
              >
                <Bookmark className="size-4" />
                <span className="hidden xl:inline">Coleção</span>
              </Link>
              <Link
                to="/w/$workspaceId/videos"
                params={{ workspaceId: current }}
                className={section}
                title="Vídeos"
                activeProps={{ className: "bg-zinc-100 font-medium" }}
              >
                <Clapperboard className="size-4" />
                <span className="hidden xl:inline">Vídeos</span>
              </Link>
              <Link
                to="/w/$workspaceId/resultados"
                params={{ workspaceId: current }}
                search={{}}
                className={section}
                title="Resultados"
                activeProps={{ className: "bg-zinc-100 font-medium" }}
              >
                <BarChart3 className="size-4" />
                <span className="hidden xl:inline">Resultados</span>
              </Link>
              {ws?.kind === "mentorship" && (
                <Link
                  to="/w/$workspaceId/listas"
                  params={{ workspaceId: current }}
                  className={section}
                  title="Listas"
                  activeProps={{ className: "bg-zinc-100 font-medium" }}
                >
                  <ListChecks className="size-4" />
                  <span className="hidden xl:inline">Listas</span>
                </Link>
              )}
              {showSubscription && (
                <Link
                  to="/w/$workspaceId/assinatura"
                  params={{ workspaceId: current }}
                  className={section}
                  title="Assinatura"
                  activeProps={{ className: "bg-zinc-100 font-medium" }}
                >
                  <CreditCard className="size-4" />
                  <span className="hidden xl:inline">Assinatura</span>
                </Link>
              )}
              {showCohort && (
                <Link
                  to="/w/$workspaceId/turma"
                  params={{ workspaceId: current }}
                  className={section}
                  title={ws?.kind === "personal" ? "Mentoria" : "Turma"}
                  activeProps={{ className: "bg-zinc-100 font-medium" }}
                >
                  <Users className="size-4" />
                  <span className="hidden xl:inline">{ws?.kind === "personal" ? "Mentoria" : "Turma"}</span>
                </Link>
              )}
            </nav>
          )}
          <nav className="ml-auto flex shrink-0 items-center gap-1">
            {current && (
              <Link
                to="/w/$workspaceId/notificacoes"
                params={{ workspaceId: current }}
                className="relative flex h-9 items-center rounded-lg px-2.5 hover:bg-zinc-100"
                activeProps={{ className: "bg-zinc-100" }}
                aria-label={unread ? `Notificações (${unread} não lidas)` : "Notificações"}
              >
                <Bell className="size-4" />
                {unread > 0 && (
                  <span className="absolute top-1 right-1 flex min-w-4 items-center justify-center rounded-full bg-brand px-1 text-[10px] font-semibold text-white">
                    {unread > 9 ? "9+" : unread}
                  </span>
                )}
              </Link>
            )}
            <Link
              to="/conta/shopee"
              className="flex h-9 items-center gap-2 rounded-lg px-3 text-sm hover:bg-zinc-100"
              activeProps={{ className: "bg-zinc-100" }}
            >
              <Plug className="size-4" />
              <span className="hidden sm:inline">Shopee</span>
              <span
                className={cn("size-2 rounded-full", connected ? "bg-emerald-500" : "bg-zinc-300")}
                title={connected ? "Conectada" : "Não conectada"}
              />
            </Link>
            <span className="hidden px-2 text-sm text-muted 2xl:inline">{me.data?.name}</span>
            <Button
              variant="ghost"
              size="sm"
              aria-label="Sair"
              onClick={async () => {
                await signOut();
                await navigate({ to: "/entrar" });
              }}
            >
              <LogOut className="size-4" />
            </Button>
          </nav>
        </div>
      </header>
      <div className="mx-auto flex max-w-6xl flex-col gap-4 px-4 py-6">
        <EmailVerificationNotice verified={me.data?.email_verified} />
        {ws && (ws.status === "trial" || ws.status === "suspended") && <BillingNotice ws={ws} />}
        <Outlet />
      </div>
    </div>
  );
}
