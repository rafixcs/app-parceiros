import { api, unwrap } from "@/api/client";

/** The browser has a service worker and the Push API (on the iPhone, only with the app installed on the home screen). */
export function pushSupported(): boolean {
  return typeof window !== "undefined" && "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;
}

function keyToBytes(base64url: string): Uint8Array<ArrayBuffer> {
  const pad = "=".repeat((4 - (base64url.length % 4)) % 4);
  const b64 = (base64url + pad).replace(/-/g, "+").replace(/_/g, "/");
  const bin = atob(b64);
  const out = new Uint8Array(new ArrayBuffer(bin.length));
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

async function registration(): Promise<ServiceWorkerRegistration> {
  const r = await navigator.serviceWorker.getRegistration();
  if (r) return r;
  return navigator.serviceWorker.ready;
}

/** This browser's subscription, if any. */
export async function currentSubscription(): Promise<PushSubscription | null> {
  if (!pushSupported()) return null;
  const r = await navigator.serviceWorker.getRegistration();
  return (await r?.pushManager.getSubscription()) ?? null;
}

/** Asks for permission, subscribes the browser and sends the subscription to the API. */
export async function enablePush(publicKey: string): Promise<void> {
  if (!pushSupported()) throw new Error("Este navegador não recebe notificações. No iPhone, instale o app na tela inicial.");
  const permission = await Notification.requestPermission();
  if (permission !== "granted") throw new Error("O navegador não deu permissão para notificações. Libere nas configurações do site.");
  const r = await registration();
  let sub = await r.pushManager.getSubscription();
  if (!sub) {
    sub = await r.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyToBytes(publicKey) });
  }
  const json = sub.toJSON();
  await unwrap(
    api.POST("/v1/me/push", {
      body: { endpoint: sub.endpoint, keys: { p256dh: json.keys?.p256dh ?? "", auth: json.keys?.auth ?? "" } },
    }),
  );
}

/** Cancels the subscription in this browser and in the API. */
export async function disablePush(): Promise<void> {
  const sub = await currentSubscription();
  if (!sub) return;
  await unwrap(api.DELETE("/v1/me/push", { body: { endpoint: sub.endpoint } }));
  await sub.unsubscribe();
}
