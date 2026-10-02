import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";
import { VitePWA } from "vite-plugin-pwa";

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    VitePWA({
      registerType: "autoUpdate",
      manifest: {
        name: "App Parceiros",
        short_name: "Parceiros",
        description: "Produtos em alta da Shopee para afiliados",
        lang: "pt-BR",
        theme_color: "#ee4d2d",
        background_color: "#ffffff",
        display: "standalone",
        icons: [{ src: "/icone.svg", sizes: "any", type: "image/svg+xml", purpose: "any maskable" }],
      },
      // A API nunca vai para o cache do service worker. O push-sw.js mostra
      // as notificações de Web Push.
      workbox: { navigateFallbackDenylist: [/^\/v1\//], importScripts: ["/push-sw.js"] },
      // O service worker também roda no `npm run dev`, para testar o push localmente.
      devOptions: { enabled: true, type: "classic", navigateFallbackAllowlist: [/^\/$/] },
    }),
  ],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  server: {
    port: 5173,
    // Em dev, a API (porta 8080 do Tilt) responde na mesma origem.
    proxy: { "/v1": "http://localhost:8080" },
  },
});
