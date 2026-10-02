import { UserManager, WebStorageStateStore } from "oidc-client-ts";

// Modo "dev": a API (AUTH_MODE=dev) aceita "Bearer dev:<nome>", sem senha.
// Modo "oidc": login no Zitadel com PKCE; o token de acesso vai para a API.
export const modoAuth: "dev" | "oidc" = import.meta.env.VITE_AUTH_MODE === "oidc" ? "oidc" : "dev";

const chaveDev = "parceiros.token-dev";

const oidc =
  modoAuth === "oidc"
    ? new UserManager({
        authority: import.meta.env.VITE_OIDC_ISSUER,
        client_id: import.meta.env.VITE_OIDC_CLIENT_ID,
        redirect_uri: `${window.location.origin}/callback`,
        post_logout_redirect_uri: window.location.origin,
        scope: "openid profile email",
        userStore: new WebStorageStateStore({ store: window.localStorage }),
        automaticSilentRenew: true,
      })
    : null;

export async function token(): Promise<string | null> {
  if (oidc) {
    const u = await oidc.getUser();
    return u && !u.expired ? u.access_token : null;
  }
  try {
    return localStorage.getItem(chaveDev);
  } catch {
    return null;
  }
}

export async function entrar(nomeDev?: string): Promise<void> {
  if (oidc) return oidc.signinRedirect();
  const nome = (nomeDev ?? "").trim().toLowerCase().replace(/[^a-z0-9._-]/g, "");
  if (!nome) throw new Error("Informe um nome para entrar.");
  localStorage.setItem(chaveDev, `dev:${nome}`);
}

export async function concluirLogin(): Promise<void> {
  if (oidc) await oidc.signinRedirectCallback();
}

export async function sair(): Promise<void> {
  if (oidc) return oidc.signoutRedirect();
  localStorage.removeItem(chaveDev);
}
