import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Bell, BellOff, Mail } from "lucide-react";
import { useEffect, useState } from "react";
import { api, exigir, type Notificacao } from "@/api/cliente";
import { Button } from "@/components/ui/button";
import { Aviso, Card } from "@/components/ui/card";
import { haQuanto } from "@/lib/formato";
import { ativarPush, desativarPush, inscricaoAtual, pushSuportado } from "@/lib/push";
import { cn } from "@/lib/utils";
import { useNaoLidas } from "./listas-api";
import { rotaNotificacoes } from "./router";

export function Notificacoes() {
  const { workspaceId } = rotaNotificacoes.useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const caixa = useNaoLidas(workspaceId);
  const invalidar = () => qc.invalidateQueries({ queryKey: ["notificacoes", workspaceId] });

  const lerTodas = useMutation({
    mutationFn: () =>
      exigir(api.POST("/v1/workspaces/{workspaceId}/notificacoes/lidas", { params: { path: { workspaceId } } })),
    onSuccess: invalidar,
  });
  const abrir = async (n: Notificacao) => {
    if (!n.lida_em) {
      await exigir(
        api.POST("/v1/workspaces/{workspaceId}/notificacoes/{notificacaoId}/lida", {
          params: { path: { workspaceId, notificacaoId: n.id } },
        }),
      ).catch(() => undefined);
      void invalidar();
    }
    if (n.url.startsWith("/")) await navigate({ to: n.url });
  };

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <h1 className="text-2xl font-semibold">Notificações</h1>
          <p className="text-sm text-suave">Avisos deste workspace, como as listas novas do seu mentor.</p>
        </div>
        {!!caixa.data?.nao_lidas && (
          <Button variante="secundario" tamanho="sm" disabled={lerTodas.isPending} onClick={() => lerTodas.mutate()}>
            Marcar todas como lidas
          </Button>
        )}
      </div>

      <Preferencias />

      {caixa.error && <Aviso className="border-red-200 bg-red-50 text-red-800">{caixa.error.message}</Aviso>}
      {caixa.data?.notificacoes.length === 0 && (
        <Card className="p-8 text-center text-sm text-suave">Nenhuma notificação por aqui.</Card>
      )}
      <ul className="flex flex-col gap-2">
        {caixa.data?.notificacoes.map((n) => (
          <li key={n.id}>
            <button type="button" className="w-full text-left" onClick={() => void abrir(n)}>
              <Card className={cn("flex gap-3 p-4 hover:border-zinc-300", !n.lida_em && "border-marca/40 bg-orange-50/40")}>
                <span className={cn("mt-1.5 size-2 shrink-0 rounded-full", n.lida_em ? "bg-transparent" : "bg-marca")} />
                <span className="min-w-0 flex-1">
                  <span className="block text-sm font-medium">{n.titulo}</span>
                  {n.corpo && <span className="mt-0.5 block text-sm text-suave">{n.corpo}</span>}
                  <span className="mt-1 block text-xs text-suave">{haQuanto(n.criado_em)}</span>
                </span>
              </Card>
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

function Preferencias() {
  const qc = useQueryClient();
  const prefs = useQuery({ queryKey: ["prefs-notificacao"], queryFn: () => exigir(api.GET("/v1/eu/notificacoes")) });
  const [nesteNavegador, setNesteNavegador] = useState<boolean | null>(null);
  useEffect(() => {
    void inscricaoAtual().then((s) => setNesteNavegador(!!s));
  }, []);

  const email = useMutation({
    mutationFn: (v: boolean) => exigir(api.PUT("/v1/eu/notificacoes", { body: { email: v } })),
    onSuccess: (p) => qc.setQueryData(["prefs-notificacao"], p),
  });
  const push = useMutation({
    mutationFn: async (ligar: boolean) => {
      if (ligar) await ativarPush(prefs.data!.push_chave_publica!);
      else await desativarPush();
      return ligar;
    },
    onSuccess: (ligar) => {
      setNesteNavegador(ligar);
      void qc.invalidateQueries({ queryKey: ["prefs-notificacao"] });
    },
  });

  if (!prefs.data) return null;
  const p = prefs.data;
  return (
    <Card className="flex flex-col gap-3 p-4 text-sm">
      <label className="flex items-center gap-3">
        <Mail className="size-4 text-suave" />
        <span className="flex-1">Receber por e-mail</span>
        <input
          type="checkbox"
          className="size-4"
          checked={p.email}
          disabled={email.isPending}
          onChange={(e) => email.mutate(e.target.checked)}
        />
      </label>
      <div className="flex items-center gap-3">
        {nesteNavegador ? <Bell className="size-4 text-suave" /> : <BellOff className="size-4 text-suave" />}
        <span className="flex-1">
          Notificações neste navegador
          {!p.push_chave_publica && <span className="block text-xs text-suave">Indisponíveis neste servidor.</span>}
          {p.push_chave_publica && !pushSuportado() && (
            <span className="block text-xs text-suave">
              Este navegador não recebe notificações. No iPhone, instale o app na tela inicial.
            </span>
          )}
        </span>
        {p.push_chave_publica && pushSuportado() && nesteNavegador !== null && (
          <Button
            tamanho="sm"
            variante={nesteNavegador ? "secundario" : "primario"}
            disabled={push.isPending}
            onClick={() => push.mutate(!nesteNavegador)}
          >
            {nesteNavegador ? "Desativar" : "Ativar"}
          </Button>
        )}
      </div>
      {(email.error || push.error) && <p className="text-red-700">{(email.error ?? push.error)?.message}</p>}
    </Card>
  );
}
