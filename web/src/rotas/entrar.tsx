import { useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input, Rotulo } from "@/components/ui/input";
import { concluirLogin, entrar, modoAuth } from "@/lib/auth";

export function Entrar() {
  const navigate = useNavigate();
  const [nome, setNome] = useState("");
  const [erro, setErro] = useState("");

  async function enviar(e: React.FormEvent) {
    e.preventDefault();
    try {
      await entrar(nome);
      await navigate({ to: "/" });
    } catch (err) {
      setErro((err as Error).message);
    }
  }

  return (
    <main className="flex min-h-dvh items-center justify-center p-4">
      <Card className="w-full max-w-sm p-6">
        <img src="/icone.svg" alt="" className="mb-4 size-10" />
        <h1 className="text-xl font-semibold">App Parceiros</h1>
        <p className="mt-1 text-sm text-suave">Produtos em alta da Shopee para você divulgar.</p>
        {modoAuth === "oidc" ? (
          <Button className="mt-6 w-full" onClick={() => entrar()}>
            Entrar
          </Button>
        ) : (
          <form onSubmit={enviar} className="mt-6 flex flex-col gap-3">
            <Rotulo texto="Ambiente local: entrar como">
              <Input value={nome} onChange={(e) => setNome(e.target.value)} placeholder="ex.: ana" autoFocus />
            </Rotulo>
            {erro && <p className="text-sm text-red-700">{erro}</p>}
            <Button type="submit">Entrar</Button>
          </form>
        )}
      </Card>
    </main>
  );
}

export function Callback() {
  const navigate = useNavigate();
  const [erro, setErro] = useState("");
  useEffect(() => {
    concluirLogin()
      .then(() => navigate({ to: "/" }))
      .catch(() => setErro("Não foi possível concluir o login. Tente de novo."));
  }, [navigate]);
  return <main className="p-8 text-center text-sm text-suave">{erro || "Entrando…"}</main>;
}
