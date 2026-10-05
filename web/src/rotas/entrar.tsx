import { Link, useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input, Rotulo } from "@/components/ui/input";
import { authProvider, completeSignIn, signInDev, signInWithProvider } from "@/lib/auth";
import { InternalSignIn } from "./internal-auth";

/** Sign-in page: the form depends on the identity provider of the build. */
export function SignIn() {
  return (
    <AuthCard subtitle="Produtos em alta da Shopee para você divulgar.">
      {authProvider === "oidc" && (
        <Button className="mt-6 w-full" onClick={() => void signInWithProvider()}>
          Entrar
        </Button>
      )}
      {authProvider === "internal" && <InternalSignIn />}
      {authProvider === "dev" && <DevSignIn />}
    </AuthCard>
  );
}

/** The card shared by the sign-in, sign-up and password pages. */
export function AuthCard({ subtitle, children }: { subtitle: string; children: React.ReactNode }) {
  return (
    <main className="flex min-h-dvh items-center justify-center p-4">
      <Card className="w-full max-w-sm p-6">
        <Link to="/entrar">
          <img src="/icone.svg" alt="" className="mb-4 size-10" />
        </Link>
        <h1 className="text-xl font-semibold">App Parceiros</h1>
        <p className="mt-1 text-sm text-suave">{subtitle}</p>
        {children}
      </Card>
    </main>
  );
}

function DevSignIn() {
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [error, setError] = useState("");

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    try {
      signInDev(name);
      await navigate({ to: "/" });
    } catch (err) {
      setError((err as Error).message);
    }
  }

  return (
    <form onSubmit={submit} className="mt-6 flex flex-col gap-3">
      <Rotulo texto="Ambiente local: entrar como">
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="ex.: ana" autoFocus />
      </Rotulo>
      {error && <p className="text-sm text-red-700">{error}</p>}
      <Button type="submit">Entrar</Button>
    </form>
  );
}

export function Callback() {
  const navigate = useNavigate();
  const [error, setError] = useState("");
  useEffect(() => {
    completeSignIn()
      .then(() => navigate({ to: "/" }))
      .catch(() => setError("Não foi possível concluir o login. Tente de novo."));
  }, [navigate]);
  return <main className="p-8 text-center text-sm text-suave">{error || "Entrando…"}</main>;
}
