import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { api, exigir } from "@/api/cliente";
import { Button } from "@/components/ui/button";
import { Aviso } from "@/components/ui/card";
import { Input, Rotulo } from "@/components/ui/input";
import { authProvider, storeSession } from "@/lib/auth";
import { AuthCard } from "./entrar";
import { rotaRedefinirSenha, rotaVerificarEmail } from "./router";

// Screens of the internal identity provider (AUTH_PROVIDER=internal): sign-in,
// sign-up, email verification and password reset. The API answers the error
// messages in pt-BR, ready to show.

function ErrorText({ error }: { error: unknown }) {
  if (!error) return null;
  return <p className="text-sm text-red-700">{(error as Error).message}</p>;
}

export function InternalSignIn() {
  const navigate = useNavigate();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const signIn = useMutation({
    mutationFn: () => exigir(api.POST("/v1/auth/login", { body: { email, password } })),
    onSuccess: async (s) => {
      storeSession(s.token);
      await navigate({ to: "/" });
    },
  });

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        signIn.mutate();
      }}
      className="mt-6 flex flex-col gap-3"
    >
      <Rotulo texto="E-mail">
        <Input type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} required autoFocus />
      </Rotulo>
      <Rotulo texto="Senha">
        <Input
          type="password"
          autoComplete="current-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
        />
      </Rotulo>
      <ErrorText error={signIn.error} />
      <Button type="submit" disabled={signIn.isPending}>
        Entrar
      </Button>
      <div className="flex justify-between text-sm">
        <Link to="/cadastro" className="text-marca hover:underline">
          Criar conta
        </Link>
        <Link to="/esqueci-a-senha" className="text-suave hover:underline">
          Esqueci a senha
        </Link>
      </div>
    </form>
  );
}

export function SignUp() {
  const navigate = useNavigate();
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const signUp = useMutation({
    mutationFn: () => exigir(api.POST("/v1/auth/register", { body: { name, email, password } })),
    onSuccess: async (s) => {
      storeSession(s.token);
      await navigate({ to: "/" });
    },
  });

  return (
    <AuthCard subtitle="Crie a sua conta. Enviamos um link para confirmar o e-mail.">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          signUp.mutate();
        }}
        className="mt-6 flex flex-col gap-3"
      >
        <Rotulo texto="Nome">
          <Input autoComplete="name" value={name} onChange={(e) => setName(e.target.value)} maxLength={80} required autoFocus />
        </Rotulo>
        <Rotulo texto="E-mail">
          <Input type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
        </Rotulo>
        <Rotulo texto="Senha (mínimo de 8 caracteres)">
          <Input
            type="password"
            autoComplete="new-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            minLength={8}
            maxLength={128}
            required
          />
        </Rotulo>
        <ErrorText error={signUp.error} />
        <Button type="submit" disabled={signUp.isPending}>
          Criar conta
        </Button>
        <p className="text-sm text-suave">
          Já tem conta?{" "}
          <Link to="/entrar" className="text-marca hover:underline">
            Entrar
          </Link>
        </p>
      </form>
    </AuthCard>
  );
}

export function ForgotPassword() {
  const [email, setEmail] = useState("");
  const request = useMutation({
    mutationFn: () => exigir(api.POST("/v1/auth/password/forgot", { body: { email } })),
  });

  return (
    <AuthCard subtitle="Informe o seu e-mail para receber o link de redefinição da senha.">
      {request.isSuccess ? (
        <p className="mt-6 text-sm">
          Se houver uma conta com {email}, você vai receber o link em instantes. Ele vale por 1 hora.
        </p>
      ) : (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            request.mutate();
          }}
          className="mt-6 flex flex-col gap-3"
        >
          <Rotulo texto="E-mail">
            <Input type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} required autoFocus />
          </Rotulo>
          <ErrorText error={request.error} />
          <Button type="submit" disabled={request.isPending}>
            Enviar link
          </Button>
        </form>
      )}
      <Link to="/entrar" className="mt-4 block text-sm text-marca hover:underline">
        Voltar para o login
      </Link>
    </AuthCard>
  );
}

export function ResetPassword() {
  const { token } = rotaRedefinirSenha.useSearch();
  const [password, setPassword] = useState("");
  const reset = useMutation({
    mutationFn: () => exigir(api.POST("/v1/auth/password/reset", { body: { token, password } })),
  });

  return (
    <AuthCard subtitle="Escolha uma nova senha. As sessões abertas em outros aparelhos serão encerradas.">
      {reset.isSuccess ? (
        <p className="mt-6 text-sm">
          Senha trocada.{" "}
          <Link to="/entrar" className="text-marca hover:underline">
            Entre com a nova senha
          </Link>
          .
        </p>
      ) : (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            reset.mutate();
          }}
          className="mt-6 flex flex-col gap-3"
        >
          <Rotulo texto="Nova senha (mínimo de 8 caracteres)">
            <Input
              type="password"
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              minLength={8}
              maxLength={128}
              required
              autoFocus
            />
          </Rotulo>
          <ErrorText error={reset.error} />
          <Button type="submit" disabled={reset.isPending || !token}>
            Trocar senha
          </Button>
        </form>
      )}
    </AuthCard>
  );
}

export function VerifyEmail() {
  const { token } = rotaVerificarEmail.useSearch();
  const qc = useQueryClient();
  const verify = useMutation({
    mutationFn: () => exigir(api.POST("/v1/auth/email/verify", { body: { token } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["eu"] }),
  });
  // The link is opened once; StrictMode would call the effect twice.
  const sent = useRef(false);
  useEffect(() => {
    if (!sent.current && token) {
      sent.current = true;
      verify.mutate();
    }
  }, [token, verify]);

  return (
    <AuthCard subtitle="Confirmação do e-mail">
      <div className="mt-6 text-sm">
        {verify.isSuccess && <p>E-mail confirmado. Obrigado!</p>}
        {verify.isPending && <p className="text-suave">Confirmando…</p>}
        <ErrorText error={verify.error ?? (!token ? new Error("Link incompleto. Abra de novo o link do e-mail.") : null)} />
      </div>
      <Link to="/" className="mt-4 block text-sm text-marca hover:underline">
        Ir para o app
      </Link>
    </AuthCard>
  );
}

/** Shown in the app while the email of an internal account is unconfirmed. */
export function EmailVerificationNotice({ verified }: { verified: boolean | undefined }) {
  const resend = useMutation({ mutationFn: () => exigir(api.POST("/v1/auth/email/resend")) });
  if (authProvider !== "internal" || verified !== false) return null;
  return (
    <Aviso className="flex flex-wrap items-center gap-2 border-amber-200 bg-amber-50 text-amber-900">
      <span className="flex-1">
        {resend.isSuccess
          ? "Enviamos um novo link. Confira a sua caixa de entrada."
          : "Confirme o seu e-mail para receber convites de mentoria e avisos por e-mail."}
      </span>
      {!resend.isSuccess && (
        <Button variante="secundario" tamanho="sm" disabled={resend.isPending} onClick={() => resend.mutate()}>
          Reenviar link
        </Button>
      )}
      <ErrorText error={resend.error} />
    </Aviso>
  );
}
