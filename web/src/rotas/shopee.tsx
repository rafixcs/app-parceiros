import { useMutation, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, ShieldCheck } from "lucide-react";
import { useState } from "react";
import { api, exigir } from "@/api/cliente";
import { Button } from "@/components/ui/button";
import { Aviso, Card } from "@/components/ui/card";
import { Input, Rotulo } from "@/components/ui/input";
import { haQuanto } from "@/lib/formato";
import { useConexaoShopee } from "./layout";

const textosStatus = {
  conectado: "Conectada",
  invalido: "A Shopee recusou a credencial. Gere um novo Secret e conecte de novo.",
  expirado: "A Shopee negou acesso à conta. Confira se o acesso à Open API continua aprovado.",
  desconectado: "Não conectada",
} as const;

export function ConexaoShopee() {
  const qc = useQueryClient();
  const conexao = useConexaoShopee();
  const [appId, setAppId] = useState("");
  const [secret, setSecret] = useState("");

  const conectar = useMutation({
    mutationFn: () => exigir(api.PUT("/v1/eu/shopee", { body: { app_id: appId.trim(), secret } })),
    onSuccess: (c) => {
      qc.setQueryData(["shopee"], c);
      setAppId("");
      setSecret("");
    },
  });
  const desconectar = useMutation({
    mutationFn: () => exigir(api.DELETE("/v1/eu/shopee")),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["shopee"] }),
  });

  const c = conexao.data;
  const conectado = c?.status === "conectado";

  return (
    <div className="mx-auto flex max-w-xl flex-col gap-4">
      <div>
        <h1 className="text-2xl font-semibold">Conta de afiliado Shopee</h1>
        <p className="text-sm text-suave">
          Com a sua credencial da Open API, os links dos produtos que você salvar saem no seu ID de afiliado.
          Ela vale em todos os seus workspaces.
        </p>
      </div>

      {c && (
        <Card className="flex items-start gap-3 p-4">
          <CheckCircle2 className={conectado ? "size-5 text-emerald-600" : "size-5 text-zinc-300"} />
          <div className="flex-1 text-sm">
            <p className="font-medium">{textosStatus[c.status]}</p>
            {c.app_id && (
              <p className="text-suave">
                AppID {c.app_id}
                {c.verificado_em && <> · verificada {haQuanto(c.verificado_em)}</>}
              </p>
            )}
          </div>
          {c.status !== "desconectado" && (
            <Button variante="perigo" tamanho="sm" disabled={desconectar.isPending} onClick={() => desconectar.mutate()}>
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
            conectar.mutate();
          }}
        >
          <h2 className="font-medium">{conectado ? "Trocar credencial" : "Conectar"}</h2>
          <p className="text-sm text-suave">
            Copie o AppID e o Secret no painel de afiliados da Shopee, em Open API. Testamos a credencial com a
            Shopee antes de salvar.
          </p>
          <Rotulo texto="AppID">
            <Input inputMode="numeric" autoComplete="off" value={appId} onChange={(e) => setAppId(e.target.value)} required />
          </Rotulo>
          <Rotulo texto="Secret">
            <Input type="password" autoComplete="off" value={secret} onChange={(e) => setSecret(e.target.value)} required />
          </Rotulo>
          {conectar.error && <Aviso className="border-red-200 bg-red-50 text-red-800">{conectar.error.message}</Aviso>}
          <Button type="submit" disabled={conectar.isPending}>
            {conectar.isPending ? "Testando com a Shopee…" : "Conectar"}
          </Button>
          <p className="flex items-center gap-1.5 text-xs text-suave">
            <ShieldCheck className="size-4" /> O Secret é guardado criptografado e nunca é mostrado de volta.
          </p>
        </form>
      </Card>
    </div>
  );
}
