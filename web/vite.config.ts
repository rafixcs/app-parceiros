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
        icons: [{ src: "/icon.svg", sizes: "any", type: "image/svg+xml", purpose: "any maskable" }],
      },
      // The API never goes to the service worker cache. push-sw.js shows the
      // Web Push notifications.
      workbox: { navigateFallbackDenylist: [/^\/v1\//], importScripts: ["/push-sw.js"] },
      // The service worker also runs on `npm run dev`, to test push locally.
      devOptions: { enabled: true, type: "classic", navigateFallbackAllowlist: [/^\/$/] },
    }),
  ],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  server: {
    port: 5173,
    // In dev, the API (Tilt's port 8080) answers on the same origin.
    proxy: { "/v1": "http://localhost:8080" },
  },
});
