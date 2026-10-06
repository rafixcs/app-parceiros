import { Navigate } from "@tanstack/react-router";
import { Notice } from "@/components/ui/card";
import { pendingInvite } from "./invite";
import { lastWorkspace, useWorkspaces } from "./layout";

/**
 * Opens the radar of the last workspace used or, without one, of the first (the personal one).
 * Whoever signed in from an invite goes back to it.
 */
export function Home() {
  const { data, error } = useWorkspaces();
  const invite = pendingInvite();
  if (invite) return <Navigate to="/convite/$token" params={{ token: invite }} replace />;
  if (error) return <Notice className="border-red-200 bg-red-50 text-red-800">{error.message}</Notice>;
  const last = lastWorkspace();
  const first = data?.find((w) => w.id === last) ?? data?.[0];
  if (!first) return <p className="text-sm text-muted">Carregando…</p>;
  return <Navigate to="/w/$workspaceId/radar" params={{ workspaceId: first.id }} search={{}} replace />;
}
