import createClient, { type Middleware } from "openapi-fetch";
import { token } from "@/lib/auth";
import type { components, paths } from "./schema";

export type Schemas = components["schemas"];
export type RadarItem = Schemas["RadarItem"];
export type Workspace = Schemas["Workspace"];
export type Item = Schemas["Item"];
export type Colecao = Schemas["Colecao"];

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

export const api = createClient<paths>({ baseUrl: "" });
api.use(autenticar);

/** Devolve `data` ou lança ErroAPI. Para usar nas queries do TanStack Query. */
export async function exigir<T>(
  p: Promise<{ data?: T; error?: unknown; response: Response }>,
): Promise<T> {
  const { data, error, response } = await p;
  if (response.ok) return data as T;
  const e = (error ?? {}) as Partial<Schemas["Erro"]>;
  throw new ErroAPI(response.status, e.codigo ?? "erro", e.mensagem ?? "Algo deu errado. Tente de novo.");
}
