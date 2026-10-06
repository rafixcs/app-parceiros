import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { api, ApiError, unwrap } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Card, Notice } from "@/components/ui/card";
import { token as authToken } from "@/lib/auth";
import { inviteRoute } from "./router";

const pendingKey = "parceiros.convite";

/** Invite opened before signing in: after signing in, the home page goes back to it. */
export function pendingInvite(): string | null {
  try {
    return sessionStorage.getItem(pendingKey);
  } catch {
    return null;
  }
}

function storePending(t: string | null) {
  try {
    if (t) sessionStorage.setItem(pendingKey, t);
    else sessionStorage.removeItem(pendingKey);
  } catch {
    // Without sessionStorage, the person just has to open the link again after signing in.
  }
}

const statusTexts = {
  expired: "Este convite expirou. Peça um novo ao seu mentor.",
  used: "Este convite já foi usado. Peça um novo ao seu mentor.",
  revoked: "Este convite foi cancelado. Peça um novo ao seu mentor.",
} as const;

export function Invite() {
  const { token } = inviteRoute.useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [signedIn, setSignedIn] = useState<boolean | null>(null);
  useEffect(() => {
    void authToken().then((t) => {
      setSignedIn(!!t);
      // Already signed in on this invite: the home page no longer needs to come back to it.
      if (t) storePending(null);
    });
  }, []);

  const invite = useQuery({
    queryKey: ["invite", token],
    queryFn: () => unwrap(api.GET("/v1/invites/{token}", { params: { path: { token } } })),
    retry: false,
  });
  const accept = useMutation({
    mutationFn: () => unwrap(api.POST("/v1/invites/{token}/accept", { params: { path: { token } } })),
    onSuccess: async (w) => {
      storePending(null);
      await qc.invalidateQueries({ queryKey: ["workspaces"] });
      await navigate({ to: "/w/$workspaceId/listas", params: { workspaceId: w.id } });
    },
  });
  const alreadyMember = accept.error instanceof ApiError && accept.error.code === "already_member";

  return (
    <main className="flex min-h-dvh items-center justify-center p-4">
      <Card className="flex w-full max-w-sm flex-col gap-4 p-6">
        <img src="/icon.svg" alt="" className="size-10" />
        {invite.isPending && <p className="text-sm text-muted">Abrindo convite…</p>}
        {invite.error && <Notice className="border-red-200 bg-red-50 text-red-800">{invite.error.message}</Notice>}
        {invite.data && (
          <>
            <div>
              <p className="text-sm text-muted">Convite para a mentoria</p>
              <h1 className="text-xl font-semibold">{invite.data.workspace_name}</h1>
            </div>
            {invite.data.status !== "valid" ? (
              <Notice className="border-amber-200 bg-amber-50 text-amber-800">{statusTexts[invite.data.status]}</Notice>
            ) : signedIn === false ? (
              <Button
                onClick={() => {
                  storePending(token);
                  void navigate({ to: "/entrar" });
                }}
              >
                Entrar para aceitar
              </Button>
            ) : (
              <Button disabled={accept.isPending || signedIn === null} onClick={() => accept.mutate()}>
                {accept.isPending ? "Entrando na mentoria…" : "Aceitar convite"}
              </Button>
            )}
            {accept.error && !alreadyMember && <p className="text-sm text-red-700">{accept.error.message}</p>}
            {alreadyMember && (
              <p className="text-sm text-muted">
                Você já participa desta mentoria. <Link to="/" className="text-brand hover:underline">Abrir o app</Link>
              </p>
            )}
          </>
        )}
      </Card>
    </main>
  );
}
