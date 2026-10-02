import { useQuery } from "@tanstack/react-query";
import { Link, Outlet, useNavigate, useParams } from "@tanstack/react-router";
import { Bell, Bookmark, Flame, ListChecks, LogOut, Plug, Users } from "lucide-react";
import { useEffect } from "react";
import { api, exigir } from "@/api/cliente";
import { Button } from "@/components/ui/button";
import { Select } from "@/components/ui/input";
import { sair } from "@/lib/auth";
import { cn } from "@/lib/utils";
import { useNaoLidas } from "./listas-api";

const chaveUltimo = "parceiros.workspace";

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
  const naoLidas = useNaoLidas(atual || undefined).data?.nao_lidas ?? 0;
  const secao = "flex h-9 items-center gap-2 rounded-lg px-3 text-sm hover:bg-zinc-100";

  return (
    <div className="min-h-dvh">
      <header className="sticky top-0 z-10 border-b border-borda bg-white/90 backdrop-blur">
        <div className="mx-auto flex max-w-6xl items-center gap-3 px-4 py-2">
          <Link to="/" className="flex shrink-0 items-center gap-2 font-semibold">
            <img src="/icone.svg" alt="" className="size-7" />
            <span className="hidden sm:inline">Parceiros</span>
          </Link>
          {workspaces.data && workspaces.data.length > 0 && (
            <Select
              aria-label="Workspace"
              className="h-9 max-w-56"
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
            <nav className="flex items-center gap-1" aria-label="Seções">
              <Link
                to="/w/$workspaceId/radar"
                params={{ workspaceId: atual }}
                search={{}}
                className={secao}
                activeProps={{ className: "bg-zinc-100 font-medium" }}
              >
                <Flame className="size-4" />
                <span className="hidden sm:inline">Radar</span>
              </Link>
              <Link
                to="/w/$workspaceId/colecao"
                params={{ workspaceId: atual }}
                search={{}}
                className={secao}
                activeProps={{ className: "bg-zinc-100 font-medium" }}
              >
                <Bookmark className="size-4" />
                <span className="hidden sm:inline">Coleção</span>
              </Link>
              {ws?.tipo === "mentoria" && (
                <Link
                  to="/w/$workspaceId/listas"
                  params={{ workspaceId: atual }}
                  className={secao}
                  activeProps={{ className: "bg-zinc-100 font-medium" }}
                >
                  <ListChecks className="size-4" />
                  <span className="hidden sm:inline">Listas</span>
                </Link>
              )}
              {verTurma && (
                <Link
                  to="/w/$workspaceId/turma"
                  params={{ workspaceId: atual }}
                  className={secao}
                  activeProps={{ className: "bg-zinc-100 font-medium" }}
                >
                  <Users className="size-4" />
                  <span className="hidden lg:inline">{ws?.tipo === "pessoal" ? "Mentoria" : "Turma"}</span>
                </Link>
              )}
            </nav>
          )}
          <nav className="ml-auto flex items-center gap-1">
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
            <span className="hidden px-2 text-sm text-suave md:inline">{eu.data?.nome}</span>
            <Button
              variante="fantasma"
              tamanho="sm"
              aria-label="Sair"
              onClick={async () => {
                await sair();
                await navigate({ to: "/entrar" });
              }}
            >
              <LogOut className="size-4" />
            </Button>
          </nav>
        </div>
      </header>
      <div className="mx-auto max-w-6xl px-4 py-6">
        <Outlet />
      </div>
    </div>
  );
}
