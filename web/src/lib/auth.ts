import { UserManager, WebStorageStateStore } from "oidc-client-ts";

// The identity provider is chosen at build time and must match the API's
// AUTH_PROVIDER:
// - "dev": the API accepts "Bearer dev:<name>", without a password.
// - "oidc": sign-in at Zitadel with PKCE; the access token goes to the API.
// - "internal": email and password in the app itself; the API returns an
//   opaque session token.
export type AuthProvider = "dev" | "oidc" | "internal";

const configured = import.meta.env.VITE_AUTH_PROVIDER;
export const authProvider: AuthProvider = configured === "oidc" || configured === "internal" ? configured : "dev";

const devKey = "parceiros.token-dev";
const sessionKey = "parceiros.session";

const oidc =
  authProvider === "oidc"
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

function read(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function write(key: string, value: string | null) {
  try {
    if (value) localStorage.setItem(key, value);
    else localStorage.removeItem(key);
  } catch {
    // Without localStorage the session lasts until the page reloads.
  }
}

/** Returns the access token of the signed-in user, or null. */
export async function token(): Promise<string | null> {
  if (oidc) {
    const u = await oidc.getUser();
    return u && !u.expired ? u.access_token : null;
  }
  return read(authProvider === "internal" ? sessionKey : devKey);
}

/** Starts the sign-in at the external provider (redirect). */
export async function signInWithProvider(): Promise<void> {
  if (oidc) await oidc.signinRedirect();
}

/** Signs in locally as `name` (dev provider). */
export function signInDev(name: string) {
  const clean = name.trim().toLowerCase().replace(/[^a-z0-9._-]/g, "");
  if (!clean) throw new Error("Informe um nome para entrar.");
  write(devKey, `dev:${clean}`);
}

/** Keeps the session token returned by the internal provider. */
export function storeSession(sessionToken: string) {
  write(sessionKey, sessionToken);
}

export async function completeSignIn(): Promise<void> {
  if (oidc) await oidc.signinRedirectCallback();
}

export async function signOut(): Promise<void> {
  if (oidc) return oidc.signoutRedirect();
  if (authProvider === "internal") {
    const t = read(sessionKey);
    write(sessionKey, null);
    // Best effort: the session is gone from this browser either way.
    if (t) await fetch("/v1/auth/logout", { method: "POST", headers: { Authorization: `Bearer ${t}` } }).catch(() => {});
    return;
  }
  write(devKey, null);
}

/** Forgets an internal session the API no longer accepts. */
export function dropSession() {
  if (authProvider === "internal") write(sessionKey, null);
}
