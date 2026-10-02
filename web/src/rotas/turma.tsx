import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Copy, Mail, UserMinus, X } from "lucide-react";
import { useState } from "react";
import { api, exigir } from "@/api/cliente";
import { Button } from "@/components/ui/button";
import { Aviso, Badge, Card } from "@/components/ui/card";
import { Input, Rotulo } from "@/components/ui/input";
import { copiar } from "@/lib/copiar";
import { useWorkspaceAtual } from "./listas-api";
import { rotaTurma } from "./router";

const papeis = { dono: "Dono", mentor: "Mentor", afiliado: "Afiliado" } as const;

export function Turma() {
  const { workspaceId } = rotaTurma.useParams();
  const { workspace, gestor, carregando } = useWorkspaceAtual(workspaceId);

  if (carregando) return <p className="text-sm text-suave">Carregando…</p>;
  return (
    <div className="flex flex-col gap-5">
      <div>
        <h1 className="text-2xl font-semibold">{workspace?.tipo === "mentoria" ? workspace.nome : "Mentoria"}</h1>
        <p className="text-sm text-suave">
          {workspace?.tipo === "mentoria"
            ? "Convide afiliados e veja quem está na turma."
            : "Crie uma mentoria para montar listas de produtos e convidar a sua turma."}
        </p>
      </div>
      {workspace?.tipo !== "mentoria" && <NovaMentoria />}
      {workspace?.tipo === "mentoria" && gestor && (
        <>
          <Convidar workspaceId={workspaceId} />
          <Convites workspaceId={workspaceId} />
          <Membros workspaceId={workspaceId} />
        </>
      )}
      {workspace?.tipo === "mentoria" && !gestor && (
        <Card className="p-4 text-sm text-suave">Você participa desta mentoria como afiliado.</Card>
      )}
    </div>
  );
}

function NovaMentoria() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const [nome, setNome] = useState("");
  const criar = useMutation({
    mutationFn: () => exigir(api.POST("/v1/workspaces", { body: { nome } })),
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
          criar.mutate();
        }}
      >
        <Rotulo texto="Nome da mentoria">
          <Input value={nome} onChange={(e) => setNome(e.target.value)} maxLength={80} placeholder="ex.: Turma da Ana" required />
        </Rotulo>
        <Button type="submit" disabled={criar.isPending}>
          {criar.isPending ? "Criando…" : "Criar mentoria"}
        </Button>
      </form>
      {criar.error && <p className="mt-2 text-sm text-red-700">{criar.error.message}</p>}
    </Card>
  );
}

function Convidar({ workspaceId }: { workspaceId: string }) {
  const qc = useQueryClient();
  const [email, setEmail] = useState("");
  const [copiado, setCopiado] = useState<boolean | null>(null);
  const criar = useMutation({
    mutationFn: () =>
      exigir(
        api.POST("/v1/workspaces/{workspaceId}/convites", {
          params: { path: { workspaceId } },
          body: { email: email.trim() || null },
        }),
      ),
    onSuccess: () => {
      setEmail("");
      setCopiado(null);
      void qc.invalidateQueries({ queryKey: ["convites", workspaceId] });
    },
  });
  const c = criar.data;
  return (
    <Card className="flex flex-col gap-3 p-4">
      <p className="font-medium">Convidar afiliado</p>
      <form
        className="flex flex-col gap-2 sm:flex-row"
        onSubmit={(e) => {
          e.preventDefault();
          criar.mutate();
        }}
      >
        <Input
          type="email"
          aria-label="E-mail do afiliado"
          placeholder="E-mail (opcional; sem e-mail, qualquer pessoa com o link entra)"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />
        <Button type="submit" disabled={criar.isPending} className="shrink-0">
          <Mail className="size-4" /> {criar.isPending ? "Gerando…" : "Gerar convite"}
        </Button>
      </form>
      {criar.error && <p className="text-sm text-red-700">{criar.error.message}</p>}
      {c && (
        <Aviso className="border-emerald-200 bg-emerald-50 text-emerald-900">
          <p>
            {c.email_enviado === true && `Convite enviado para ${c.email}. `}
            {c.email_enviado === false && "Não conseguimos enviar o e-mail agora. "}
            Envie este link ao afiliado (ele só aparece agora e vale uma vez):
          </p>
          <div className="mt-2 flex gap-2">
            <Input readOnly value={c.url} onFocus={(e) => e.target.select()} className="bg-white" />
            <Button variante="secundario" onClick={async () => setCopiado(await copiar(c.url))} className="shrink-0">
              <Copy className="size-4" /> {copiado ? "Copiado!" : "Copiar"}
            </Button>
          </div>
        </Aviso>
      )}
    </Card>
  );
}

function Convites({ workspaceId }: { workspaceId: string }) {
  const qc = useQueryClient();
  const convites = useQuery({
    queryKey: ["convites", workspaceId],
    queryFn: () =>
      exigir(api.GET("/v1/workspaces/{workspaceId}/convites", { params: { path: { workspaceId } } })),
  });
  const revogar = useMutation({
    mutationFn: (conviteId: string) =>
      exigir(
        api.DELETE("/v1/workspaces/{workspaceId}/convites/{conviteId}", {
          params: { path: { workspaceId, conviteId } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["convites", workspaceId] }),
  });
  if (!convites.data?.length) return null;
  return (
    <Card className="p-4">
      <p className="mb-2 font-medium">Convites pendentes</p>
      <ul className="flex flex-col divide-y divide-borda">
        {convites.data.map((c) => (
          <li key={c.id} className="flex items-center gap-2 py-2 text-sm">
            <span className="flex-1 truncate">{c.email ?? "Link sem e-mail"}</span>
            <span className="text-xs text-suave">vale até {new Date(c.expira_em).toLocaleDateString("pt-BR")}</span>
            <Button variante="fantasma" tamanho="sm" aria-label="Cancelar convite" onClick={() => revogar.mutate(c.id)}>
              <X className="size-4" />
            </Button>
          </li>
        ))}
      </ul>
    </Card>
  );
}

function Membros({ workspaceId }: { workspaceId: string }) {
  const qc = useQueryClient();
  const membros = useQuery({
    queryKey: ["membros", workspaceId],
    queryFn: () => exigir(api.GET("/v1/workspaces/{workspaceId}/membros", { params: { path: { workspaceId } } })),
  });
  const remover = useMutation({
    mutationFn: (usuarioId: string) =>
      exigir(
        api.DELETE("/v1/workspaces/{workspaceId}/membros/{usuarioId}", {
          params: { path: { workspaceId, usuarioId } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["membros", workspaceId] }),
  });
  return (
    <Card className="p-4">
      <p className="mb-2 font-medium">Turma {membros.data && <span className="text-suave">({membros.data.length})</span>}</p>
      {remover.error && <p className="text-sm text-red-700">{remover.error.message}</p>}
      <ul className="flex flex-col divide-y divide-borda">
        {membros.data?.map((m) => (
          <li key={m.usuario_id} className="flex items-center gap-2 py-2 text-sm">
            <span className="min-w-0 flex-1">
              <span className="block truncate">{m.nome}</span>
              <span className="block truncate text-xs text-suave">{m.email}</span>
            </span>
            <Badge className="bg-zinc-100 text-zinc-700">{papeis[m.papel]}</Badge>
            {m.papel === "afiliado" && (
              <Button
                variante="fantasma"
                tamanho="sm"
                aria-label={`Remover ${m.nome}`}
                onClick={() => {
                  if (window.confirm(`Remover ${m.nome} da turma? A coleção e a conta da Shopee continuam com quem sai.`))
                    remover.mutate(m.usuario_id);
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
