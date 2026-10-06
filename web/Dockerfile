# syntax=docker/dockerfile:1

# Production front end: the Vite build served by nginx on the API's domain
# (the Ingress sends /v1 to the API and everything else here). The VITE_*
# variables go into the build, so an image serves a single environment.
FROM node:22-bookworm-slim AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
ARG VITE_AUTH_PROVIDER=oidc
ARG VITE_OIDC_ISSUER
ARG VITE_OIDC_CLIENT_ID
RUN test "$VITE_AUTH_PROVIDER" != oidc || { test -n "$VITE_OIDC_ISSUER" && test -n "$VITE_OIDC_CLIENT_ID"; } \
 || { echo "defina VITE_OIDC_ISSUER e VITE_OIDC_CLIENT_ID" >&2; exit 1; }
RUN npm run build

FROM nginxinc/nginx-unprivileged:1.29-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/dist /usr/share/nginx/html
EXPOSE 8080
