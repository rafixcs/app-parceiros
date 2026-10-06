import createClient, { type Middleware } from "openapi-fetch";
import { dropSession, token } from "@/lib/auth";
import type { components, paths } from "./schema";

export type Schemas = components["schemas"];
export type Trend = Schemas["Trend"];
export type Workspace = Schemas["Workspace"];
export type Item = Schemas["Item"];
export type Collection = Schemas["Collection"];
export type CuratedList = Schemas["CuratedList"];
export type CuratedListDetail = Schemas["CuratedListDetail"];
export type CuratedListItem = Schemas["CuratedListItem"];
export type Notification = Schemas["Notification"];
export type Video = Schemas["Video"];

/** API error with the stable code and the message ready to show to the user. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message);
  }
}

const authenticate: Middleware = {
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
api.use(authenticate, sessionExpired);

/** Returns `data` or throws ApiError. Meant for TanStack Query queries. */
export async function unwrap<T>(
  p: Promise<{ data?: T; error?: unknown; response: Response }>,
): Promise<T> {
  const { data, error, response } = await p;
  if (response.ok) return data as T;
  const e = (error ?? {}) as Partial<Schemas["Error"]>;
  throw new ApiError(response.status, e.code ?? "error", e.message ?? "Algo deu errado. Tente de novo.");
}
