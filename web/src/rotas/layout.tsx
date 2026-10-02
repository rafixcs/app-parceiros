import { useQuery } from "@tanstack/react-query";
import { Link, Outlet, useNavigate, useParams } from "@tanstack/react-router";
import { LogOut, Plug } from "lucide-react";
import { useEffect } from "react";
import { api, exigir } from "@/api/cliente";
import { Button } from "@/components/ui/button";
import { Select } from "@/components/ui/input";
import { sair } from "@/lib/auth";
import { cn } from "@/lib/utils";

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
          <nav className="ml-auto flex items-center gap-1">
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
