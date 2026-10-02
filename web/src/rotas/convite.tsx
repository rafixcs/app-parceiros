import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { api, ErroAPI, exigir } from "@/api/cliente";
import { Button } from "@/components/ui/button";
import { Aviso, Card } from "@/components/ui/card";
import { token as tokenAuth } from "@/lib/auth";
import { rotaConvite } from "./router";

const chavePendente = "parceiros.convite";

/** Convite aberto antes do login: depois de entrar, o início volta para ele. */
export function convitePendente(): string | null {
  try {
    return sessionStorage.getItem(chavePendente);
  } catch {
    return null;
  }
}

function guardarPendente(t: string | null) {
  try {
    if (t) sessionStorage.setItem(chavePendente, t);
    else sessionStorage.removeItem(chavePendente);
  } catch {
    // Sem sessionStorage, a pessoa só precisa abrir o link de novo depois de entrar.
  }
}

const situacoes = {
  expirado: "Este convite expirou. Peça um novo ao seu mentor.",
  usado: "Este convite já foi usado. Peça um novo ao seu mentor.",
  revogado: "Este convite foi cancelado. Peça um novo ao seu mentor.",
} as const;

export function Convite() {
  const { token } = rotaConvite.useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [logado, setLogado] = useState<boolean | null>(null);
  useEffect(() => {
    void tokenAuth().then((t) => {
      setLogado(!!t);
      // Já logado neste convite: o início não precisa mais voltar para ele.
      if (t) guardarPendente(null);
    });
  }, []);

  const convite = useQuery({
    queryKey: ["convite", token],
    queryFn: () => exigir(api.GET("/v1/convites/{token}", { params: { path: { token } } })),
    retry: false,
  });
  const aceitar = useMutation({
    mutationFn: () => exigir(api.POST("/v1/convites/{token}/aceitar", { params: { path: { token } } })),
    onSuccess: async (w) => {
      guardarPendente(null);
      await qc.invalidateQueries({ queryKey: ["workspaces"] });
      await navigate({ to: "/w/$workspaceId/listas", params: { workspaceId: w.id } });
    },
  });
  const jaMembro = aceitar.error instanceof ErroAPI && aceitar.error.codigo === "ja_membro";

  return (
    <main className="flex min-h-dvh items-center justify-center p-4">
      <Card className="flex w-full max-w-sm flex-col gap-4 p-6">
        <img src="/icone.svg" alt="" className="size-10" />
        {convite.isPending && <p className="text-sm text-suave">Abrindo convite…</p>}
        {convite.error && <Aviso className="border-red-200 bg-red-50 text-red-800">{convite.error.message}</Aviso>}
        {convite.data && (
          <>
            <div>
              <p className="text-sm text-suave">Convite para a mentoria</p>
              <h1 className="text-xl font-semibold">{convite.data.workspace_nome}</h1>
            </div>
            {convite.data.status !== "valido" ? (
              <Aviso className="border-amber-200 bg-amber-50 text-amber-800">{situacoes[convite.data.status]}</Aviso>
            ) : logado === false ? (
              <Button
                onClick={() => {
                  guardarPendente(token);
                  void navigate({ to: "/entrar" });
                }}
              >
                Entrar para aceitar
              </Button>
            ) : (
              <Button disabled={aceitar.isPending || logado === null} onClick={() => aceitar.mutate()}>
                {aceitar.isPending ? "Entrando na mentoria…" : "Aceitar convite"}
              </Button>
            )}
            {aceitar.error && !jaMembro && <p className="text-sm text-red-700">{aceitar.error.message}</p>}
            {jaMembro && (
              <p className="text-sm text-suave">
                Você já participa desta mentoria. <Link to="/" className="text-marca hover:underline">Abrir o app</Link>
              </p>
            )}
          </>
        )}
      </Card>
    </main>
  );
}
