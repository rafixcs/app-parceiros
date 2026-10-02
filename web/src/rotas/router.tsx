import { createRootRoute, createRoute, createRouter, lazyRouteComponent, redirect } from "@tanstack/react-router";
import { token } from "@/lib/auth";
import { Colecao, validarBuscaColecao } from "./colecao";
import { Callback, Entrar } from "./entrar";
import { Inicio } from "./inicio";
import { Layout } from "./layout";
import { Radar, validarBusca } from "./radar";
import { ConexaoShopee } from "./shopee";

const raiz = createRootRoute();

const entrar = createRoute({ getParentRoute: () => raiz, path: "/entrar", component: Entrar });
const callback = createRoute({ getParentRoute: () => raiz, path: "/callback", component: Callback });

// Tudo abaixo exige login.
const app = createRoute({
  getParentRoute: () => raiz,
  id: "app",
  component: Layout,
  beforeLoad: async () => {
    if (!(await token())) throw redirect({ to: "/entrar" });
  },
});

const inicio = createRoute({ getParentRoute: () => app, path: "/", component: Inicio });

export const rotaRadar = createRoute({
  getParentRoute: () => app,
  path: "/w/$workspaceId/radar",
  validateSearch: validarBusca,
  component: Radar,
});

export const rotaProduto = createRoute({
  getParentRoute: () => app,
  path: "/w/$workspaceId/radar/$produtoId",
  // O gráfico (recharts) só carrega quando alguém abre um produto.
  component: lazyRouteComponent(() => import("./produto"), "Produto"),
});

export const rotaColecao = createRoute({
  getParentRoute: () => app,
  path: "/w/$workspaceId/colecao",
  validateSearch: validarBuscaColecao,
  component: Colecao,
});

export const rotaItem = createRoute({
  getParentRoute: () => app,
  path: "/w/$workspaceId/colecao/$itemId",
  component: lazyRouteComponent(() => import("./item"), "Item"),
});

const shopee = createRoute({ getParentRoute: () => app, path: "/conta/shopee", component: ConexaoShopee });

export const router = createRouter({
  routeTree: raiz.addChildren([entrar, callback, app.addChildren([inicio, rotaRadar, rotaProduto, rotaColecao, rotaItem, shopee])]),
  defaultPreload: "intent",
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
