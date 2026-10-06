import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Copy, Mail, UserMinus, X } from "lucide-react";
import { useState } from "react";
import { api, unwrap, type Schemas } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Notice, Badge, Card } from "@/components/ui/card";
import { Field, Input } from "@/components/ui/input";
import { copyText } from "@/lib/clipboard";
import { useCurrentWorkspace } from "./layout";
import { cohortRoute } from "./router";

const roles: Record<Schemas["Role"], string> = { owner: "Dono", mentor: "Mentor", affiliate: "Afiliado" };

// An invite is valid for 7 days.
const INVITE_VALIDITY_HOURS = 168;

export function Cohort() {
  const { workspaceId } = cohortRoute.useParams();
  const { workspace, manager, loading } = useCurrentWorkspace(workspaceId);

  if (loading) return <p className="text-sm text-muted">Carregando…</p>;
  const mentorship = workspace?.kind === "mentorship";
  return (
    <div className="flex flex-col gap-5">
      <div>
        <h1 className="text-2xl font-semibold">{mentorship ? workspace.name : "Mentoria"}</h1>
        <p className="text-sm text-muted">
          {mentorship
            ? "Convide afiliados e veja quem está na turma."
            : "Crie uma mentoria para montar listas de produtos e convidar a sua turma."}
        </p>
      </div>
      {!mentorship && <NewMentorship />}
      {mentorship && manager && (
        <>
          <InviteForm workspaceId={workspaceId} />
          <Invites workspaceId={workspaceId} />
          <Members workspaceId={workspaceId} />
        </>
      )}
      {mentorship && !manager && (
        <Card className="p-4 text-sm text-muted">Você participa desta mentoria como afiliado.</Card>
      )}
    </div>
  );
}

function NewMentorship() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const create = useMutation({
    mutationFn: () => unwrap(api.POST("/v1/workspaces", { body: { name } })),
    onSuccess: async (w) => {
      await qc.invalidateQueries({ queryKey: ["workspaces"] });
      await navigate({ to: "/w/$workspaceId/turma", params: { workspaceId: w.id } });
    },
  });
  return (
    <Card className="p-4">
      <form
        className="flex flex-col gap-3 sm:flex-row sm:items-end"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate();
        }}
      >
        <Field label="Nome da mentoria">
          <Input value={name} onChange={(e) => setName(e.target.value)} maxLength={80} placeholder="ex.: Turma da Ana" required />
        </Field>
        <Button type="submit" disabled={create.isPending}>
          {create.isPending ? "Criando…" : "Criar mentoria"}
        </Button>
      </form>
      {create.error && <p className="mt-2 text-sm text-red-700">{create.error.message}</p>}
    </Card>
  );
}

function InviteForm({ workspaceId }: { workspaceId: string }) {
  const qc = useQueryClient();
  const [email, setEmail] = useState("");
  const [copied, setCopied] = useState<boolean | null>(null);
  const create = useMutation({
    mutationFn: () =>
      unwrap(
        api.POST("/v1/workspaces/{workspaceId}/invites", {
          params: { path: { workspaceId } },
          body: { email: email.trim() || null, validity_hours: INVITE_VALIDITY_HOURS },
        }),
      ),
    onSuccess: () => {
      setEmail("");
      setCopied(null);
      void qc.invalidateQueries({ queryKey: ["invites", workspaceId] });
    },
  });
  const c = create.data;
  return (
    <Card className="flex flex-col gap-3 p-4">
      <p className="font-medium">Convidar afiliado</p>
      <form
        className="flex flex-col gap-2 sm:flex-row"
        onSubmit={(e) => {
          e.preventDefault();
          create.mutate();
        }}
      >
        <Input
          type="email"
          aria-label="E-mail do afiliado"
          placeholder="E-mail (opcional; sem e-mail, qualquer pessoa com o link entra)"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />
        <Button type="submit" disabled={create.isPending} className="shrink-0">
          <Mail className="size-4" /> {create.isPending ? "Gerando…" : "Gerar convite"}
        </Button>
      </form>
      {create.error && <p className="text-sm text-red-700">{create.error.message}</p>}
      {c && (
        <Notice className="border-emerald-200 bg-emerald-50 text-emerald-900">
          <p>
            {c.email_sent === true && `Convite enviado para ${c.email}. `}
            {c.email_sent === false && "Não conseguimos enviar o e-mail agora. "}
            Envie este link ao afiliado (ele só aparece agora e vale uma vez):
          </p>
          <div className="mt-2 flex gap-2">
            <Input readOnly value={c.url} onFocus={(e) => e.target.select()} className="bg-white" />
            <Button variant="secondary" onClick={async () => setCopied(await copyText(c.url))} className="shrink-0">
              <Copy className="size-4" /> {copied ? "Copiado!" : "Copiar"}
            </Button>
          </div>
        </Notice>
      )}
    </Card>
  );
}

function Invites({ workspaceId }: { workspaceId: string }) {
  const qc = useQueryClient();
  const invites = useQuery({
    queryKey: ["invites", workspaceId],
    queryFn: () => unwrap(api.GET("/v1/workspaces/{workspaceId}/invites", { params: { path: { workspaceId } } })),
  });
  const revoke = useMutation({
    mutationFn: (inviteId: string) =>
      unwrap(
        api.DELETE("/v1/workspaces/{workspaceId}/invites/{inviteId}", {
          params: { path: { workspaceId, inviteId } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["invites", workspaceId] }),
  });
  if (!invites.data?.length) return null;
  return (
    <Card className="p-4">
      <p className="mb-2 font-medium">Convites pendentes</p>
      <ul className="flex flex-col divide-y divide-border">
        {invites.data.map((c) => (
          <li key={c.id} className="flex items-center gap-2 py-2 text-sm">
            <span className="flex-1 truncate">{c.email ?? "Link sem e-mail"}</span>
            <span className="text-xs text-muted">vale até {new Date(c.expires_at).toLocaleDateString("pt-BR")}</span>
            <Button variant="ghost" size="sm" aria-label="Cancelar convite" onClick={() => revoke.mutate(c.id)}>
              <X className="size-4" />
            </Button>
          </li>
        ))}
      </ul>
    </Card>
  );
}

function Members({ workspaceId }: { workspaceId: string }) {
  const qc = useQueryClient();
  const members = useQuery({
    queryKey: ["members", workspaceId],
    queryFn: () => unwrap(api.GET("/v1/workspaces/{workspaceId}/members", { params: { path: { workspaceId } } })),
  });
  const remove = useMutation({
    mutationFn: (userId: string) =>
      unwrap(
        api.DELETE("/v1/workspaces/{workspaceId}/members/{userId}", {
          params: { path: { workspaceId, userId } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["members", workspaceId] }),
  });
  return (
    <Card className="p-4">
      <p className="mb-2 font-medium">Turma {members.data && <span className="text-muted">({members.data.length})</span>}</p>
      {remove.error && <p className="text-sm text-red-700">{remove.error.message}</p>}
      <ul className="flex flex-col divide-y divide-border">
        {members.data?.map((m) => (
          <li key={m.user_id} className="flex items-center gap-2 py-2 text-sm">
            <span className="min-w-0 flex-1">
              <span className="block truncate">{m.name}</span>
              <span className="block truncate text-xs text-muted">{m.email}</span>
            </span>
            {m.shares_results && (
              <Badge className="bg-emerald-50 text-emerald-700" title="Autoriza somar os resultados no painel da turma">
                Mostra resultados
              </Badge>
            )}
            <Badge className="bg-zinc-100 text-zinc-700">{roles[m.role]}</Badge>
            {m.role === "affiliate" && (
              <Button
                variant="ghost"
                size="sm"
                aria-label={`Remover ${m.name}`}
                onClick={() => {
                  if (window.confirm(`Remover ${m.name} da turma? A coleção e a conta da Shopee continuam com quem sai.`))
                    remove.mutate(m.user_id);
                }}
              >
                <UserMinus className="size-4" />
              </Button>
            )}
          </li>
        ))}
      </ul>
    </Card>
  );
}
