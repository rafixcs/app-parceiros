import createClient, { type Middleware } from "openapi-fetch";
import { dropSession, token } from "@/lib/auth";
import type { components, paths } from "./schema";

export type Schemas = components["schemas"];
export type RadarItem = Schemas["RadarItem"];
export type Workspace = Schemas["Workspace"];
export type Item = Schemas["Item"];
export type Colecao = Schemas["Colecao"];
export type Lista = Schemas["Lista"];
export type ListaDetalhe = Schemas["ListaDetalhe"];
export type ItemLista = Schemas["ItemLista"];
export type Notificacao = Schemas["Notificacao"];
export type Video = Schemas["Video"];

/** Erro da API com o código estável e a mensagem pronta para o usuário. */
export class ErroAPI extends Error {
  constructor(
    readonly status: number,
    readonly codigo: string,
    mensagem: string,
  ) {
    super(mensagem);
  }
}

const autenticar: Middleware = {
  async onRequest({ request }) {
    const t = await token();
    if (t) request.headers.set("Authorization", `Bearer ${t}`);
    return request;
  },
};

// An expired or revoked internal session answers 401: forget it and go back to
// the sign-in page. The /v1/auth routes answer 401 for a wrong password, which
// the form shows itself.
const sessionExpired: Middleware = {
  async onResponse({ request, response }) {
    if (response.status === 401 && !new URL(request.url).pathname.startsWith("/v1/auth/")) {
      dropSession();
      if (window.location.pathname !== "/entrar") window.location.assign("/entrar");
    }
    return response;
  },
};

export const api = createClient<paths>({ baseUrl: "" });
api.use(autenticar, sessionExpired);

/** Devolve `data` ou lança ErroAPI. Para usar nas queries do TanStack Query. */
export async function exigir<T>(
  p: Promise<{ data?: T; error?: unknown; response: Response }>,
): Promise<T> {
  const { data, error, response } = await p;
  if (response.ok) return data as T;
  const e = (error ?? {}) as Partial<Schemas["Erro"]>;
  throw new ErroAPI(response.status, e.codigo ?? "erro", e.mensagem ?? "Algo deu errado. Tente de novo.");
}
