// Web Push do App Parceiros. O service worker gerado pelo vite-plugin-pwa
// importa este arquivo (workbox.importScripts no vite.config.ts).
// A mensagem vem do worker do backend: { titulo, corpo, url, id }.

self.addEventListener("push", (event) => {
  let msg = {};
  try {
    msg = event.data ? event.data.json() : {};
  } catch {
    msg = { titulo: event.data ? event.data.text() : "" };
  }
  const titulo = msg.titulo || "App Parceiros";
  event.waitUntil(
    self.registration.showNotification(titulo, {
      body: msg.corpo || "",
      icon: "/icone.svg",
      badge: "/icone.svg",
      tag: msg.id || undefined,
      lang: "pt-BR",
      data: { url: msg.url || "/" },
    }),
  );
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const caminho = (event.notification.data && event.notification.data.url) || "/";
  // Só caminhos do próprio app.
  const destino = new URL(caminho.startsWith("/") ? caminho : "/", self.location.origin).href;
  event.waitUntil(
    self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((janelas) => {
      for (const j of janelas) {
        if (new URL(j.url).origin === self.location.origin && "focus" in j) {
          return j.focus().then((f) => (f && "navigate" in f ? f.navigate(destino) : f));
        }
      }
      return self.clients.openWindow(destino);
    }),
  );
});
