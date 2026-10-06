import { CheckCircle2, CreditCard, ExternalLink } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Badge, Card, Notice } from "@/components/ui/card";
import { Field, Input } from "@/components/ui/input";
import { money } from "@/lib/format";
import { useCurrentWorkspace } from "./layout";
import { subscriptionRoute } from "./router";
import {
  accessStatusLabel,
  shortDate,
  useCancelSubscription,
  useChangeSeats,
  useSimulatePayment,
  useSubscribe,
  useSubscription,
  type Subscription as SubscriptionData,
} from "./subscription-api";

export function Subscription() {
  const { workspaceId } = subscriptionRoute.useParams();
  const { workspace } = useCurrentWorkspace(workspaceId);
  const { data: s, isPending, error } = useSubscription(workspaceId);
  const owner = workspace?.role === "owner";

  if (error) return <Notice className="border-red-200 bg-red-50 text-red-800">{error.message}</Notice>;
  if (isPending || !s) return <p className="text-sm text-muted">Carregando…</p>;

  const mentorship = s.plan === "mentorship";
  return (
    <div className="flex max-w-3xl flex-col gap-5">
      <div>
        <h1 className="text-2xl font-semibold">Assinatura</h1>
        <p className="text-sm text-muted">
          {mentorship
            ? "O plano da mentoria é cobrado por assento: um para cada afiliado da turma."
            : "O plano avulso é cobrado por mês, por workspace."}
        </p>
      </div>

      <AccessStatus s={s} />
      {s.status === "none" || s.status === "cancelled" ? (
        owner ? (
          <Subscribe workspaceId={workspaceId} s={s} />
        ) : (
          <Card className="p-4 text-sm text-muted">Só o dono do workspace contrata o plano.</Card>
        )
      ) : (
        <Billing workspaceId={workspaceId} s={s} owner={owner} />
      )}
    </div>
  );
}

function AccessStatus({ s }: { s: SubscriptionData }) {
  const colors = {
    active: "bg-emerald-100 text-emerald-900",
    trial: "bg-amber-100 text-amber-900",
    suspended: "bg-red-100 text-red-900",
    free: "bg-sky-100 text-sky-900",
  } as const;
  return (
    <Card className="flex flex-col gap-3 p-4">
      <div className="flex flex-wrap items-center gap-2">
        <Badge className={colors[s.access_status]}>{accessStatusLabel(s)}</Badge>
        <span className="text-sm text-muted">
          {s.access_status === "suspended"
            ? `Sem acesso desde ${shortDate(s.access_until)}.`
            : s.access_status === "free"
              ? "Enquanto você for aluno de uma mentoria em dia, este workspace não é cobrado."
              : `Acesso garantido até ${shortDate(s.access_until)}.`}
        </span>
      </div>
      <dl className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
        <div>
          <dt className="text-xs text-muted">Plano</dt>
          <dd className="font-medium">{s.plan === "mentorship" ? "Mentoria" : "Avulso"}</dd>
        </div>
        <div>
          <dt className="text-xs text-muted">{s.plan === "mentorship" ? "Por assento" : "Por mês"}</dt>
          <dd className="font-medium">{money(s.price_cents)}</dd>
        </div>
        {s.plan === "mentorship" && (
          <div>
            <dt className="text-xs text-muted">Assentos</dt>
            <dd className="font-medium">
              {s.seats_in_use} em uso{s.seats > 0 && ` de ${s.seats}`}
            </dd>
          </div>
        )}
        {s.amount_cents > 0 && (
          <div>
            <dt className="text-xs text-muted">Total por mês</dt>
            <dd className="font-medium">{money(s.amount_cents)}</dd>
          </div>
        )}
      </dl>
      {s.access_status === "suspended" && (
        <Notice className="border-red-200 bg-red-50 text-red-800">
          O workspace está suspenso: radar, coleção, listas e resultados voltam assim que o pagamento for confirmado.
          Os dados continuam todos aqui.
        </Notice>
      )}
    </Card>
  );
}

function Subscribe({ workspaceId, s }: { workspaceId: string; s: SubscriptionData }) {
  const mentorship = s.plan === "mentorship";
  const [seats, setSeats] = useState(Math.max(1, s.seats_in_use));
  const [taxId, setTaxId] = useState("");
  const subscribe = useSubscribe(workspaceId);
  const total = mentorship ? seats * s.price_cents : s.price_cents;

  return (
    <Card className="flex flex-col gap-3 p-4">
      <p className="font-medium">Contratar o plano</p>
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          subscribe.mutate({ seats: mentorship ? seats : undefined, tax_id: taxId });
        }}
      >
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
          {mentorship && (
            <Field label="Assentos">
              <Input
                type="number"
                min={Math.max(1, s.seats_in_use)}
                max={s.max_seats}
                value={seats}
                onChange={(e) => setSeats(Number(e.target.value))}
                className="sm:w-32"
                required
              />
            </Field>
          )}
          <Field label="CPF ou CNPJ de quem paga">
            <Input
              value={taxId}
              onChange={(e) => setTaxId(e.target.value)}
              placeholder="000.000.000-00"
              inputMode="numeric"
              required
            />
          </Field>
          <Button type="submit" disabled={subscribe.isPending} className="shrink-0">
            <CreditCard className="size-4" />
            {subscribe.isPending ? "Gerando cobrança…" : `Assinar por ${money(total)}/mês`}
          </Button>
        </div>
        <p className="text-xs text-muted">
          A cobrança é mensal e você escolhe PIX, boleto ou cartão na página de pagamento. O acesso é liberado assim
          que o pagamento é confirmado.
        </p>
      </form>
      {subscribe.error && <p className="text-sm text-red-700">{subscribe.error.message}</p>}
    </Card>
  );
}

function Billing({ workspaceId, s, owner }: { workspaceId: string; s: SubscriptionData; owner: boolean }) {
  const changeSeats = useChangeSeats(workspaceId);
  const cancel = useCancelSubscription(workspaceId);
  const simulate = useSimulatePayment(workspaceId);
  const [seats, setSeats] = useState(s.seats);
  const [confirming, setConfirming] = useState(false);
  const error = changeSeats.error ?? cancel.error ?? simulate.error;

  return (
    <Card className="flex flex-col gap-4 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <p className="font-medium">
            {s.status === "active" && "Pagamento em dia"}
            {s.status === "pending" && "Aguardando o pagamento"}
            {s.status === "overdue" && "Pagamento em atraso"}
          </p>
          {s.next_due_date && (
            <p className="text-sm text-muted">
              {s.status === "active" ? "Próxima cobrança em " : "Vencimento em "}
              {shortDate(s.next_due_date)}
            </p>
          )}
        </div>
        {s.payment_url && (
          <Button onClick={() => window.open(s.payment_url ?? "", "_blank", "noopener")}>
            <ExternalLink className="size-4" /> Abrir a fatura
          </Button>
        )}
        {s.simulated && (
          <Button variant="secondary" disabled={simulate.isPending} onClick={() => simulate.mutate()}>
            <CheckCircle2 className="size-4" /> {simulate.isPending ? "Confirmando…" : "Simular pagamento"}
          </Button>
        )}
      </div>

      {owner && s.plan === "mentorship" && (
        <form
          className="flex flex-col gap-2 border-t border-border pt-4 sm:flex-row sm:items-end"
          onSubmit={(e) => {
            e.preventDefault();
            changeSeats.mutate(seats);
          }}
        >
          <Field label="Assentos contratados">
            <Input
              type="number"
              min={1}
              max={s.max_seats}
              value={seats}
              onChange={(e) => setSeats(Number(e.target.value))}
              className="sm:w-32"
            />
          </Field>
          <Button type="submit" variant="secondary" disabled={changeSeats.isPending || seats === s.seats}>
            {changeSeats.isPending ? "Salvando…" : "Mudar assentos"}
          </Button>
          <p className="text-xs text-muted sm:pb-3">
            Menos assentos valem na hora; mais assentos, quando o próximo pagamento for confirmado.
          </p>
        </form>
      )}

      {owner && (
        <div className="border-t border-border pt-4">
          {confirming ? (
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-sm">
                Cancelar a assinatura? O acesso continua até{" "}
                {s.next_due_date ? shortDate(s.next_due_date) : "o fim do ciclo pago"}.
              </span>
              <Button
                variant="danger"
                size="sm"
                disabled={cancel.isPending}
                onClick={() => cancel.mutate(undefined, { onSettled: () => setConfirming(false) })}
              >
                {cancel.isPending ? "Cancelando…" : "Cancelar assinatura"}
              </Button>
              <Button variant="ghost" size="sm" onClick={() => setConfirming(false)}>
                Manter
              </Button>
            </div>
          ) : (
            <Button variant="ghost" size="sm" onClick={() => setConfirming(true)}>
              Cancelar assinatura
            </Button>
          )}
        </div>
      )}
      {error && <p className="text-sm text-red-700">{error.message}</p>}
    </Card>
  );
}
