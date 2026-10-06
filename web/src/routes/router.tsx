import { createRootRoute, createRoute, createRouter, lazyRouteComponent, redirect } from "@tanstack/react-router";
import { token } from "@/lib/auth";
import { Cohort } from "./cohort";
import { CollectionPage, validateCollectionSearch } from "./collection";
import { Home } from "./home";
import { ForgotPassword, ResetPassword, SignUp, VerifyEmail } from "./internal-auth";
import { Invite } from "./invite";
import { Layout } from "./layout";
import { Lists } from "./lists";
import { Notifications } from "./notifications";
import { Radar, validateRadarSearch } from "./radar";
import { validateResultsSearch } from "./results-api";
import { ShopeeConnection } from "./shopee";
import { Callback, SignIn } from "./sign-in";
import { Subscription } from "./subscription";

// Route paths are what the customer sees in the address bar, so they stay in
// Portuguese.
const root = createRootRoute();

const signInRoute = createRoute({ getParentRoute: () => root, path: "/entrar", component: SignIn });
const callbackRoute = createRoute({ getParentRoute: () => root, path: "/callback", component: Callback });

// Internal identity provider (email and password). The links sent by email
// carry the one-time token in ?token=.
const validateTokenSearch = (s: Record<string, unknown>) => ({ token: typeof s.token === "string" ? s.token : "" });
const signUpRoute = createRoute({ getParentRoute: () => root, path: "/cadastro", component: SignUp });
const forgotPasswordRoute = createRoute({ getParentRoute: () => root, path: "/esqueci-a-senha", component: ForgotPassword });
export const resetPasswordRoute = createRoute({
  getParentRoute: () => root,
  path: "/redefinir-senha",
  validateSearch: validateTokenSearch,
  component: ResetPassword,
});
export const verifyEmailRoute = createRoute({
  getParentRoute: () => root,
  path: "/verificar-email",
  validateSearch: validateTokenSearch,
  component: VerifyEmail,
});

// The invite opens without signing in: it shows the mentorship and leads to the sign-in to accept.
export const inviteRoute = createRoute({ getParentRoute: () => root, path: "/convite/$token", component: Invite });

// Everything below requires signing in.
const appRoute = createRoute({
  getParentRoute: () => root,
  id: "app",
  component: Layout,
  beforeLoad: async () => {
    if (!(await token())) throw redirect({ to: "/entrar" });
  },
});

const homeRoute = createRoute({ getParentRoute: () => appRoute, path: "/", component: Home });

export const radarRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/w/$workspaceId/radar",
  validateSearch: validateRadarSearch,
  component: Radar,
});

export const productRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/w/$workspaceId/radar/$productId",
  // The chart (recharts) only loads when someone opens a product.
  component: lazyRouteComponent(() => import("./product"), "ProductPage"),
});

export const collectionRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/w/$workspaceId/colecao",
  validateSearch: validateCollectionSearch,
  component: CollectionPage,
});

export const itemRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/w/$workspaceId/colecao/$itemId",
  component: lazyRouteComponent(() => import("./item"), "ItemPage"),
});

export const listsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/w/$workspaceId/listas",
  component: Lists,
});

export const listRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/w/$workspaceId/listas/$listId",
  component: lazyRouteComponent(() => import("./list"), "ListPage"),
});

export const cohortRoute = createRoute({ getParentRoute: () => appRoute, path: "/w/$workspaceId/turma", component: Cohort });

export const videosRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/w/$workspaceId/videos",
  component: lazyRouteComponent(() => import("./videos"), "Videos"),
});

// The charts (recharts) only load when someone opens the results.
export const resultsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/w/$workspaceId/resultados",
  validateSearch: validateResultsSearch,
  component: lazyRouteComponent(() => import("./results"), "Results"),
});

export const cohortResultsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/w/$workspaceId/resultados/turma",
  validateSearch: validateResultsSearch,
  component: lazyRouteComponent(() => import("./results"), "CohortResults"),
});

export const subscriptionRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/w/$workspaceId/assinatura",
  component: Subscription,
});

export const notificationsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/w/$workspaceId/notificacoes",
  component: Notifications,
});

const shopeeRoute = createRoute({ getParentRoute: () => appRoute, path: "/conta/shopee", component: ShopeeConnection });

export const router = createRouter({
  routeTree: root.addChildren([
    signInRoute,
    callbackRoute,
    signUpRoute,
    forgotPasswordRoute,
    resetPasswordRoute,
    verifyEmailRoute,
    inviteRoute,
    appRoute.addChildren([
      homeRoute,
      radarRoute,
      productRoute,
      collectionRoute,
      itemRoute,
      listsRoute,
      listRoute,
      cohortRoute,
      videosRoute,
      resultsRoute,
      cohortResultsRoute,
      subscriptionRoute,
      notificationsRoute,
      shopeeRoute,
    ]),
  ]),
  defaultPreload: "intent",
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
