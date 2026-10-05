# syntax=docker/dockerfile:1

# Front de produção: build do Vite servido pelo nginx no mesmo domínio da API
# (o Ingress manda /v1 para a API e o resto para cá). As variáveis VITE_* entram
# no build, então a imagem é de um ambiente só.
FROM node:22-bookworm-slim AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
ARG VITE_AUTH_MODE=oidc
ARG VITE_OIDC_ISSUER
ARG VITE_OIDC_CLIENT_ID
RUN test "$VITE_AUTH_MODE" = dev || { test -n "$VITE_OIDC_ISSUER" && test -n "$VITE_OIDC_CLIENT_ID"; } \
 || { echo "defina VITE_OIDC_ISSUER e VITE_OIDC_CLIENT_ID" >&2; exit 1; }
RUN npm run build

FROM nginxinc/nginx-unprivileged:1.29-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/dist /usr/share/nginx/html
EXPOSE 8080
