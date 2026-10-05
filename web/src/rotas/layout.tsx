import { useQuery } from "@tanstack/react-query";
import { Link, Outlet, useNavigate, useParams } from "@tanstack/react-router";
import { BarChart3, Bell, Bookmark, Clapperboard, CreditCard, Flame, ListChecks, LogOut, Plug, Users } from "lucide-react";
import { useEffect } from "react";
import { api, exigir, type Workspace } from "@/api/cliente";
import { Button } from "@/components/ui/button";
import { Aviso } from "@/components/ui/card";
import { Select } from "@/components/ui/input";
import { signOut } from "@/lib/auth";
import { cn } from "@/lib/utils";
import { diasAte } from "./assinatura-api";
import { useNaoLidas } from "./listas-api";
import { EmailVerificationNotice } from "./internal-auth";

const chaveUltimo = "parceiros.workspace";

/**
 * Aviso de cobrança no topo do app: o teste terminando e o workspace suspenso.
 * O afiliado não paga nada, então para ele o aviso só diz com quem falar.
 */
function AvisoCobranca({ ws }: { ws: Workspace }) {
  const dias = diasAte(ws.acesso_ate);
  const dono = ws.papel === "dono";
  const suspenso = ws.status === "suspenso";
  if (!suspenso && dias > 3) return null;
  return (
    <Aviso
      className={cn(
        "flex flex-wrap items-center gap-2",
        suspenso ? "border-red-200 bg-red-50 text-red-800" : "border-amber-200 bg-amber-50 text-amber-900",
      )}
    >
      <span>
        {suspenso
          ? "Este workspace está suspenso por falta de pagamento."
          : dias <= 0
            ? "O período de teste terminou."
            : `O período de teste termina em ${dias === 1 ? "1 dia" : `${dias} dias`}.`}
        {!dono && " Fale com o dono do workspace."}
      </span>
      {dono && (
        <Link
          to="/w/$workspaceId/assinatura"
          params={{ workspaceId: ws.id }}
          className="font-medium underline underline-offset-2"
        >
          Cuidar da assinatura
        </Link>
      )}
    </Aviso>
  );
}

/** Último workspace aberto, para voltar a ele fora das rotas de workspace. */
export function ultimoWorkspace(): string | null {
  try {
    return localStorage.getItem(chaveUltimo);
  } catch {
    return null;
  }
}

export function useWorkspaces() {
  return useQuery({ queryKey: ["workspaces"], queryFn: () => exigir(api.GET("/v1/workspaces")) });
}

export function useConexaoShopee() {
  return useQuery({ queryKey: ["shopee"], queryFn: () => exigir(api.GET("/v1/eu/shopee")) });
}

export function Layout() {
  const navigate = useNavigate();
  const { workspaceId } = useParams({ strict: false });
  const workspaces = useWorkspaces();
  const conexao = useConexaoShopee();
  const eu = useQuery({ queryKey: ["eu"], queryFn: () => exigir(api.GET("/v1/eu")) });

  const conectado = conexao.data?.status === "conectado";
  useEffect(() => {
    try {
      if (workspaceId) localStorage.setItem(chaveUltimo, workspaceId);
    } catch {
      // Sem localStorage, o seletor só não lembra a escolha.
    }
  }, [workspaceId]);
  const atual = workspaceId ?? ultimoWorkspace() ?? "";
  const ws = workspaces.data?.find((w) => w.id === atual);
  // Na mentoria, só dono e mentor gerem a turma; no pessoal, o link leva a criar uma mentoria.
  const verTurma = ws?.tipo === "pessoal" || ws?.papel === "dono" || ws?.papel === "mentor";
  // A assinatura é do dono (o mentor acompanha); o afiliado não paga nada.
  const verAssinatura = ws?.papel === "dono" || ws?.papel === "mentor";
  const naoLidas = useNaoLidas(atual || undefined).data?.nao_lidas ?? 0;
  const secao = "flex h-9 shrink-0 items-center gap-2 rounded-lg px-3 text-sm hover:bg-zinc-100";

  return (
    <div className="min-h-dvh">
      <header className="sticky top-0 z-10 border-b border-borda bg-white/90 backdrop-blur">
        <div className="mx-auto flex max-w-6xl flex-wrap items-center gap-x-3 gap-y-1 px-4 py-2">
          <Link to="/" className="flex shrink-0 items-center gap-2 font-semibold">
            <img src="/icone.svg" alt="" className="size-7" />
            <span className="hidden 2xl:inline">Parceiros</span>
          </Link>
          {workspaces.data && workspaces.data.length > 0 && (
            <Select
              aria-label="Workspace"
              className="h-9 w-28 shrink-0 sm:w-44"
              value={atual}
              onChange={(e) =>
                navigate({ to: "/w/$workspaceId/radar", params: { workspaceId: e.target.value }, search: {} })
              }
            >
              {!atual && <option value="">Escolha um workspace</option>}
              {workspaces.data.map((w) => (
                <option key={w.id} value={w.id}>
                  {w.tipo === "pessoal" ? "Pessoal" : w.nome}
                </option>
              ))}
            </Select>
          )}
          {atual && (
            <nav className="order-last flex w-full min-w-0 items-center gap-1 overflow-x-auto [scrollbar-width:none] sm:order-none sm:w-auto" aria-label="Seções">
              <Link
                to="/w/$workspaceId/radar"
                params={{ workspaceId: atual }}
                search={{}}
                className={secao}
                title="Radar"
                activeProps={{ className: "bg-zinc-100 font-medium" }}
              >
                <Flame className="size-4" />
                <span className="hidden xl:inline">Radar</span>
              </Link>
              <Link
                to="/w/$workspaceId/colecao"
                params={{ workspaceId: atual }}
                search={{}}
                className={secao}
                title="Coleção"
                activeProps={{ className: "bg-zinc-100 font-medium" }}
              >
                <Bookmark className="size-4" />
                <span className="hidden xl:inline">Coleção</span>
              </Link>
              <Link
                to="/w/$workspaceId/videos"
                params={{ workspaceId: atual }}
                className={secao}
                title="Vídeos"
                activeProps={{ className: "bg-zinc-100 font-medium" }}
              >
                <Clapperboard className="size-4" />
                <span className="hidden xl:inline">Vídeos</span>
              </Link>
              <Link
                to="/w/$workspaceId/resultados"
                params={{ workspaceId: atual }}
                search={{}}
                className={secao}
                title="Resultados"
                activeProps={{ className: "bg-zinc-100 font-medium" }}
              >
                <BarChart3 className="size-4" />
                <span className="hidden xl:inline">Resultados</span>
              </Link>
              {ws?.tipo === "mentoria" && (
                <Link
                  to="/w/$workspaceId/listas"
                  params={{ workspaceId: atual }}
                  className={secao}
                title="Listas"
                  activeProps={{ className: "bg-zinc-100 font-medium" }}
                >
                  <ListChecks className="size-4" />
                  <span className="hidden xl:inline">Listas</span>
                </Link>
              )}
              {verAssinatura && (
                <Link
                  to="/w/$workspaceId/assinatura"
                  params={{ workspaceId: atual }}
                  className={secao}
                  title="Assinatura"
                  activeProps={{ className: "bg-zinc-100 font-medium" }}
                >
                  <CreditCard className="size-4" />
                  <span className="hidden xl:inline">Assinatura</span>
                </Link>
              )}
              {verTurma && (
                <Link
                  to="/w/$workspaceId/turma"
                  params={{ workspaceId: atual }}
                  className={secao}
                  title={ws?.tipo === "pessoal" ? "Mentoria" : "Turma"}
                  activeProps={{ className: "bg-zinc-100 font-medium" }}
                >
                  <Users className="size-4" />
                  <span className="hidden xl:inline">{ws?.tipo === "pessoal" ? "Mentoria" : "Turma"}</span>
                </Link>
              )}
            </nav>
          )}
          <nav className="ml-auto flex shrink-0 items-center gap-1">
            {atual && (
              <Link
                to="/w/$workspaceId/notificacoes"
                params={{ workspaceId: atual }}
                className="relative flex h-9 items-center rounded-lg px-2.5 hover:bg-zinc-100"
                activeProps={{ className: "bg-zinc-100" }}
                aria-label={naoLidas ? `Notificações (${naoLidas} não lidas)` : "Notificações"}
              >
                <Bell className="size-4" />
                {naoLidas > 0 && (
                  <span className="absolute top-1 right-1 flex min-w-4 items-center justify-center rounded-full bg-marca px-1 text-[10px] font-semibold text-white">
                    {naoLidas > 9 ? "9+" : naoLidas}
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
                className={cn("size-2 rounded-full", conectado ? "bg-emerald-500" : "bg-zinc-300")}
                title={conectado ? "Conectada" : "Não conectada"}
              />
            </Link>
            <span className="hidden px-2 text-sm text-suave 2xl:inline">{eu.data?.nome}</span>
            <Button
              variante="fantasma"
              tamanho="sm"
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
        <EmailVerificationNotice verified={eu.data?.email_verificado} />
        {ws && (ws.status === "teste" || ws.status === "suspenso") && <AvisoCobranca ws={ws} />}
        <Outlet />
      </div>
    </div>
  );
}
