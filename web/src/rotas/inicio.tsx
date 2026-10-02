import { Navigate } from "@tanstack/react-router";
import { Aviso } from "@/components/ui/card";
import { ultimoWorkspace, useWorkspaces } from "./layout";

/** Abre o radar do último workspace usado ou, sem ele, do primeiro (o pessoal). */
export function Inicio() {
  const { data, error } = useWorkspaces();
  if (error) return <Aviso className="border-red-200 bg-red-50 text-red-800">{error.message}</Aviso>;
  const ultimo = ultimoWorkspace();
  const primeiro = data?.find((w) => w.id === ultimo) ?? data?.[0];
  if (!primeiro) return <p className="text-sm text-suave">Carregando…</p>;
  return <Navigate to="/w/$workspaceId/radar" params={{ workspaceId: primeiro.id }} search={{}} replace />;
}
