import { api, exigir } from "@/api/cliente";

/** O navegador tem service worker e Push API (no iPhone, só com o app instalado na tela inicial). */
export function pushSuportado(): boolean {
  return typeof window !== "undefined" && "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;
}

function chaveParaBytes(base64url: string): Uint8Array<ArrayBuffer> {
  const pad = "=".repeat((4 - (base64url.length % 4)) % 4);
  const b64 = (base64url + pad).replace(/-/g, "+").replace(/_/g, "/");
  const bin = atob(b64);
  const out = new Uint8Array(new ArrayBuffer(bin.length));
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

async function registro(): Promise<ServiceWorkerRegistration> {
  const r = await navigator.serviceWorker.getRegistration();
  if (r) return r;
  return navigator.serviceWorker.ready;
}

/** A inscrição deste navegador, se houver. */
export async function inscricaoAtual(): Promise<PushSubscription | null> {
  if (!pushSuportado()) return null;
  const r = await navigator.serviceWorker.getRegistration();
  return (await r?.pushManager.getSubscription()) ?? null;
}

/** Pede permissão, inscreve o navegador e manda a inscrição para a API. */
export async function ativarPush(chavePublica: string): Promise<void> {
  if (!pushSuportado()) throw new Error("Este navegador não recebe notificações. No iPhone, instale o app na tela inicial.");
  const permissao = await Notification.requestPermission();
  if (permissao !== "granted") throw new Error("O navegador não deu permissão para notificações. Libere nas configurações do site.");
  const r = await registro();
  let sub = await r.pushManager.getSubscription();
  if (!sub) {
    sub = await r.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: chaveParaBytes(chavePublica) });
  }
  const json = sub.toJSON();
  await exigir(
    api.POST("/v1/eu/push", {
      body: { endpoint: sub.endpoint, keys: { p256dh: json.keys?.p256dh ?? "", auth: json.keys?.auth ?? "" } },
    }),
  );
}

/** Cancela a inscrição neste navegador e na API. */
export async function desativarPush(): Promise<void> {
  const sub = await inscricaoAtual();
  if (!sub) return;
  await exigir(api.DELETE("/v1/eu/push", { body: { endpoint: sub.endpoint } }));
  await sub.unsubscribe();
}
