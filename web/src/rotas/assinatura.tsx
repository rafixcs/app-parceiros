import { CheckCircle2, CreditCard, ExternalLink } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Aviso, Badge, Card } from "@/components/ui/card";
import { Input, Rotulo } from "@/components/ui/input";
import { reais } from "@/lib/formato";
import {
  dia,
  rotuloSituacao,
  useAssinar,
  useAssinatura,
  useCancelarAssinatura,
  useMudarAssentos,
  useSimularPagamento,
  type Assinatura as DadosAssinatura,
} from "./assinatura-api";
import { useWorkspaceAtual } from "./listas-api";
import { rotaAssinatura } from "./router";

export function Assinatura() {
  const { workspaceId } = rotaAssinatura.useParams();
  const { workspace } = useWorkspaceAtual(workspaceId);
  const { data: a, isPending, error } = useAssinatura(workspaceId);
  const dono = workspace?.papel === "dono";

  if (error) return <Aviso className="border-red-200 bg-red-50 text-red-800">{error.message}</Aviso>;
  if (isPending || !a) return <p className="text-sm text-suave">Carregando…</p>;

  const mentoria = a.plano === "mentoria";
  return (
    <div className="flex max-w-3xl flex-col gap-5">
      <div>
        <h1 className="text-2xl font-semibold">Assinatura</h1>
        <p className="text-sm text-suave">
          {mentoria
            ? "O plano da mentoria é cobrado por assento: um para cada afiliado da turma."
            : "O plano avulso é cobrado por mês, por workspace."}
        </p>
      </div>

      <Situacao a={a} />
      {a.status === "sem_assinatura" || a.status === "cancelada" ? (
        dono ? (
          <Contratar workspaceId={workspaceId} a={a} />
        ) : (
          <Card className="p-4 text-sm text-suave">Só o dono do workspace contrata o plano.</Card>
        )
      ) : (
        <Cobranca workspaceId={workspaceId} a={a} dono={dono} />
      )}
    </div>
  );
}

function Situacao({ a }: { a: DadosAssinatura }) {
  const cores = {
    ativo: "bg-emerald-100 text-emerald-900",
    teste: "bg-amber-100 text-amber-900",
    suspenso: "bg-red-100 text-red-900",
  } as const;
  return (
    <Card className="flex flex-col gap-3 p-4">
      <div className="flex flex-wrap items-center gap-2">
        <Badge className={cores[a.situacao]}>{rotuloSituacao(a)}</Badge>
        <span className="text-sm text-suave">
          {a.situacao === "suspenso"
            ? `Sem acesso desde ${dia(a.acesso_ate)}.`
            : `Acesso garantido até ${dia(a.acesso_ate)}.`}
        </span>
      </div>
      <dl className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
        <div>
          <dt className="text-xs text-suave">Plano</dt>
          <dd className="font-medium">{a.plano === "mentoria" ? "Mentoria" : "Avulso"}</dd>
        </div>
        <div>
          <dt className="text-xs text-suave">{a.plano === "mentoria" ? "Por assento" : "Por mês"}</dt>
          <dd className="font-medium">{reais(a.preco_centavos)}</dd>
        </div>
        {a.plano === "mentoria" && (
          <div>
            <dt className="text-xs text-suave">Assentos</dt>
            <dd className="font-medium">
              {a.assentos_em_uso} em uso{a.assentos > 0 && ` de ${a.assentos}`}
            </dd>
          </div>
        )}
        {a.valor_centavos > 0 && (
          <div>
            <dt className="text-xs text-suave">Total por mês</dt>
            <dd className="font-medium">{reais(a.valor_centavos)}</dd>
          </div>
        )}
      </dl>
      {a.situacao === "suspenso" && (
        <Aviso className="border-red-200 bg-red-50 text-red-800">
          O workspace está suspenso: radar, coleção, listas e resultados voltam assim que o pagamento for confirmado.
          Os dados continuam todos aqui.
        </Aviso>
      )}
    </Card>
  );
}

function Contratar({ workspaceId, a }: { workspaceId: string; a: DadosAssinatura }) {
  const mentoria = a.plano === "mentoria";
  const [assentos, setAssentos] = useState(Math.max(1, a.assentos_em_uso));
  const [documento, setDocumento] = useState("");
  const assinar = useAssinar(workspaceId);
  const total = mentoria ? assentos * a.preco_centavos : a.preco_centavos;

  return (
    <Card className="flex flex-col gap-3 p-4">
      <p className="font-medium">Contratar o plano</p>
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          assinar.mutate({ assentos: mentoria ? assentos : undefined, cpf_cnpj: documento });
        }}
      >
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
          {mentoria && (
            <Rotulo texto="Assentos">
              <Input
                type="number"
                min={Math.max(1, a.assentos_em_uso)}
                max={a.assentos_maximo}
                value={assentos}
                onChange={(e) => setAssentos(Number(e.target.value))}
                className="sm:w-32"
                required
              />
            </Rotulo>
          )}
          <Rotulo texto="CPF ou CNPJ de quem paga">
            <Input
              value={documento}
              onChange={(e) => setDocumento(e.target.value)}
              placeholder="000.000.000-00"
              inputMode="numeric"
              required
            />
          </Rotulo>
          <Button type="submit" disabled={assinar.isPending} className="shrink-0">
            <CreditCard className="size-4" />
            {assinar.isPending ? "Gerando cobrança…" : `Assinar por ${reais(total)}/mês`}
          </Button>
        </div>
        <p className="text-xs text-suave">
          A cobrança é mensal e você escolhe PIX, boleto ou cartão na página de pagamento. O acesso é liberado assim
          que o pagamento é confirmado.
        </p>
      </form>
      {assinar.error && <p className="text-sm text-red-700">{assinar.error.message}</p>}
    </Card>
  );
}

function Cobranca({ workspaceId, a, dono }: { workspaceId: string; a: DadosAssinatura; dono: boolean }) {
  const mudar = useMudarAssentos(workspaceId);
  const cancelar = useCancelarAssinatura(workspaceId);
  const simular = useSimularPagamento(workspaceId);
  const [assentos, setAssentos] = useState(a.assentos);
  const [confirmando, setConfirmando] = useState(false);
  const erro = mudar.error ?? cancelar.error ?? simular.error;

  return (
    <Card className="flex flex-col gap-4 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <p className="font-medium">
            {a.status === "ativa" && "Pagamento em dia"}
            {a.status === "aguardando" && "Aguardando o pagamento"}
            {a.status === "atrasada" && "Pagamento em atraso"}
          </p>
          {a.proximo_ciclo && (
            <p className="text-sm text-suave">
              {a.status === "ativa" ? "Próxima cobrança em " : "Vencimento em "}
              {dia(a.proximo_ciclo)}
            </p>
          )}
        </div>
        {a.url_pagamento && (
          <Button onClick={() => window.open(a.url_pagamento ?? "", "_blank", "noopener")}>
            <ExternalLink className="size-4" /> Abrir a fatura
          </Button>
        )}
        {a.simulavel && (
          <Button variante="secundario" disabled={simular.isPending} onClick={() => simular.mutate()}>
            <CheckCircle2 className="size-4" /> {simular.isPending ? "Confirmando…" : "Simular pagamento"}
          </Button>
        )}
      </div>

      {dono && a.plano === "mentoria" && (
        <form
          className="flex flex-col gap-2 border-t border-borda pt-4 sm:flex-row sm:items-end"
          onSubmit={(e) => {
            e.preventDefault();
            mudar.mutate(assentos);
          }}
        >
          <Rotulo texto="Assentos contratados">
            <Input
              type="number"
              min={1}
              max={a.assentos_maximo}
              value={assentos}
              onChange={(e) => setAssentos(Number(e.target.value))}
              className="sm:w-32"
            />
          </Rotulo>
          <Button type="submit" variante="secundario" disabled={mudar.isPending || assentos === a.assentos}>
            {mudar.isPending ? "Salvando…" : "Mudar assentos"}
          </Button>
          <p className="text-xs text-suave sm:pb-3">
            Menos assentos valem na hora; mais assentos, quando o próximo pagamento for confirmado.
          </p>
        </form>
      )}

      {dono && (
        <div className="border-t border-borda pt-4">
          {confirmando ? (
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-sm">
                Cancelar a assinatura? O acesso continua até {a.proximo_ciclo ? dia(a.proximo_ciclo) : "o fim do ciclo pago"}.
              </span>
              <Button
                variante="perigo"
                tamanho="sm"
                disabled={cancelar.isPending}
                onClick={() => cancelar.mutate(undefined, { onSettled: () => setConfirmando(false) })}
              >
                {cancelar.isPending ? "Cancelando…" : "Cancelar assinatura"}
              </Button>
              <Button variante="fantasma" tamanho="sm" onClick={() => setConfirmando(false)}>
                Manter
              </Button>
            </div>
          ) : (
            <Button variante="fantasma" tamanho="sm" onClick={() => setConfirmando(true)}>
              Cancelar assinatura
            </Button>
          )}
        </div>
      )}
      {erro && <p className="text-sm text-red-700">{erro.message}</p>}
    </Card>
  );
}
