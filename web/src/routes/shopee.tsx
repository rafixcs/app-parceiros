import { useMutation, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, ShieldCheck } from "lucide-react";
import { useState } from "react";
import { api, unwrap } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Card, Notice } from "@/components/ui/card";
import { Field, Input } from "@/components/ui/input";
import { timeAgo } from "@/lib/format";
import { useShopeeConnection } from "./layout";

const statusTexts = {
  connected: "Conectada",
  invalid: "A Shopee recusou a credencial. Gere um novo Secret e conecte de novo.",
  expired: "A Shopee negou acesso à conta. Confira se o acesso à Open API continua aprovado.",
  disconnected: "Não conectada",
} as const;

export function ShopeeConnection() {
  const qc = useQueryClient();
  const connection = useShopeeConnection();
  const [appId, setAppId] = useState("");
  const [secret, setSecret] = useState("");

  const connect = useMutation({
    mutationFn: () => unwrap(api.PUT("/v1/me/shopee", { body: { app_id: appId.trim(), secret } })),
    onSuccess: (c) => {
      qc.setQueryData(["shopee"], c);
      setAppId("");
      setSecret("");
    },
  });
  const disconnect = useMutation({
    mutationFn: () => unwrap(api.DELETE("/v1/me/shopee")),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["shopee"] }),
  });

  const c = connection.data;
  const connected = c?.status === "connected";

  return (
    <div className="mx-auto flex max-w-xl flex-col gap-4">
      <div>
        <h1 className="text-2xl font-semibold">Conta de afiliado Shopee</h1>
        <p className="text-sm text-muted">
          Com a sua credencial da Open API, os links dos produtos que você salvar saem no seu ID de afiliado.
          Ela vale em todos os seus workspaces.
        </p>
      </div>

      {c && (
        <Card className="flex items-start gap-3 p-4">
          <CheckCircle2 className={connected ? "size-5 text-emerald-600" : "size-5 text-zinc-300"} />
          <div className="flex-1 text-sm">
            <p className="font-medium">{statusTexts[c.status]}</p>
            {c.app_id && (
              <p className="text-muted">
                AppID {c.app_id}
                {c.verified_at && <> · verificada {timeAgo(c.verified_at)}</>}
              </p>
            )}
          </div>
          {c.status !== "disconnected" && (
            <Button variant="danger" size="sm" disabled={disconnect.isPending} onClick={() => disconnect.mutate()}>
              Desconectar
            </Button>
          )}
        </Card>
      )}

      <Card className="p-4">
        <form
          className="flex flex-col gap-3"
          onSubmit={(e) => {
            e.preventDefault();
            connect.mutate();
          }}
        >
          <h2 className="font-medium">{connected ? "Trocar credencial" : "Conectar"}</h2>
          <p className="text-sm text-muted">
            Copie o AppID e o Secret no painel de afiliados da Shopee, em Open API. Testamos a credencial com a
            Shopee antes de salvar.
          </p>
          <Field label="AppID">
            <Input inputMode="numeric" autoComplete="off" value={appId} onChange={(e) => setAppId(e.target.value)} required />
          </Field>
          <Field label="Secret">
            <Input type="password" autoComplete="off" value={secret} onChange={(e) => setSecret(e.target.value)} required />
          </Field>
          {connect.error && <Notice className="border-red-200 bg-red-50 text-red-800">{connect.error.message}</Notice>}
          <Button type="submit" disabled={connect.isPending}>
            {connect.isPending ? "Testando com a Shopee…" : "Conectar"}
          </Button>
          <p className="flex items-center gap-1.5 text-xs text-muted">
            <ShieldCheck className="size-4" /> O Secret é guardado criptografado e nunca é mostrado de volta.
          </p>
        </form>
      </Card>
    </div>
  );
}
