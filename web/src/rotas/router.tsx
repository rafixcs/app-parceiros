import { createRootRoute, createRoute, createRouter, lazyRouteComponent, redirect } from "@tanstack/react-router";
import { token } from "@/lib/auth";
import { Assinatura } from "./assinatura";
import { Colecao, validarBuscaColecao } from "./colecao";
import { Convite } from "./convite";
import { Callback, Entrar } from "./entrar";
import { Inicio } from "./inicio";
import { Layout } from "./layout";
import { Listas } from "./listas";
import { Notificacoes } from "./notificacoes";
import { Radar, validarBusca } from "./radar";
import { validarBuscaResultados } from "./resultados-api";
import { ConexaoShopee } from "./shopee";
import { Turma } from "./turma";

const raiz = createRootRoute();

const entrar = createRoute({ getParentRoute: () => raiz, path: "/entrar", component: Entrar });
const callback = createRoute({ getParentRoute: () => raiz, path: "/callback", component: Callback });

// O convite abre sem login: mostra a mentoria e leva ao login para aceitar.
export const rotaConvite = createRoute({ getParentRoute: () => raiz, path: "/convite/$token", component: Convite });

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

export const rotaListas = createRoute({
  getParentRoute: () => app,
  path: "/w/$workspaceId/listas",
  component: Listas,
});

export const rotaLista = createRoute({
  getParentRoute: () => app,
  path: "/w/$workspaceId/listas/$listaId",
  component: lazyRouteComponent(() => import("./lista"), "Lista"),
});

export const rotaTurma = createRoute({ getParentRoute: () => app, path: "/w/$workspaceId/turma", component: Turma });

export const rotaVideos = createRoute({
  getParentRoute: () => app,
  path: "/w/$workspaceId/videos",
  component: lazyRouteComponent(() => import("./videos"), "Videos"),
});

// Os gráficos (recharts) só carregam quando alguém abre os resultados.
export const rotaResultados = createRoute({
  getParentRoute: () => app,
  path: "/w/$workspaceId/resultados",
  validateSearch: validarBuscaResultados,
  component: lazyRouteComponent(() => import("./resultados"), "Resultados"),
});

export const rotaResultadosTurma = createRoute({
  getParentRoute: () => app,
  path: "/w/$workspaceId/resultados/turma",
  validateSearch: validarBuscaResultados,
  component: lazyRouteComponent(() => import("./resultados"), "ResultadosTurma"),
});

export const rotaAssinatura = createRoute({
  getParentRoute: () => app,
  path: "/w/$workspaceId/assinatura",
  component: Assinatura,
});

export const rotaNotificacoes = createRoute({
  getParentRoute: () => app,
  path: "/w/$workspaceId/notificacoes",
  component: Notificacoes,
});

const shopee = createRoute({ getParentRoute: () => app, path: "/conta/shopee", component: ConexaoShopee });

export const router = createRouter({
  routeTree: raiz.addChildren([
    entrar,
    callback,
    rotaConvite,
    app.addChildren([
      inicio,
      rotaRadar,
      rotaProduto,
      rotaColecao,
      rotaItem,
      rotaListas,
      rotaLista,
      rotaTurma,
      rotaVideos,
      rotaResultados,
      rotaResultadosTurma,
      rotaAssinatura,
      rotaNotificacoes,
      shopee,
    ]),
  ]),
  defaultPreload: "intent",
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
