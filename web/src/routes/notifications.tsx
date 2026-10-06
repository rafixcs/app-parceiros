import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Bell, BellOff, Mail } from "lucide-react";
import { useEffect, useState } from "react";
import { api, unwrap, type Notification } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Card, Notice } from "@/components/ui/card";
import { timeAgo } from "@/lib/format";
import { currentSubscription, disablePush, enablePush, pushSupported } from "@/lib/push";
import { cn } from "@/lib/utils";
import { notificationsRoute } from "./router";

/** The workspace's inbox, refreshed every minute (the header shows the unread count). */
export function useInbox(workspaceId: string | undefined) {
  return useQuery({
    queryKey: ["notifications", workspaceId],
    enabled: !!workspaceId,
    queryFn: () =>
      unwrap(
        api.GET("/v1/workspaces/{workspaceId}/notifications", { params: { path: { workspaceId: workspaceId! } } }),
      ),
    refetchInterval: 60_000,
    refetchOnWindowFocus: true,
  });
}

export function Notifications() {
  const { workspaceId } = notificationsRoute.useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const inbox = useInbox(workspaceId);
  const invalidate = () => qc.invalidateQueries({ queryKey: ["notifications", workspaceId] });

  const markAllRead = useMutation({
    mutationFn: () =>
      unwrap(api.POST("/v1/workspaces/{workspaceId}/notifications/read", { params: { path: { workspaceId } } })),
    onSuccess: invalidate,
  });
  const open = async (n: Notification) => {
    if (!n.read_at) {
      await unwrap(
        api.POST("/v1/workspaces/{workspaceId}/notifications/{notificationId}/read", {
          params: { path: { workspaceId, notificationId: n.id } },
        }),
      ).catch(() => undefined);
      void invalidate();
    }
    if (n.url.startsWith("/")) await navigate({ to: n.url });
  };

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <h1 className="text-2xl font-semibold">Notificações</h1>
          <p className="text-sm text-muted">Avisos deste workspace, como as listas novas do seu mentor.</p>
        </div>
        {!!inbox.data?.unread && (
          <Button variant="secondary" size="sm" disabled={markAllRead.isPending} onClick={() => markAllRead.mutate()}>
            Marcar todas como lidas
          </Button>
        )}
      </div>

      <Preferences />

      {inbox.error && <Notice className="border-red-200 bg-red-50 text-red-800">{inbox.error.message}</Notice>}
      {inbox.data?.notifications.length === 0 && (
        <Card className="p-8 text-center text-sm text-muted">Nenhuma notificação por aqui.</Card>
      )}
      <ul className="flex flex-col gap-2">
        {inbox.data?.notifications.map((n) => (
          <li key={n.id}>
            <button type="button" className="w-full text-left" onClick={() => void open(n)}>
              <Card className={cn("flex gap-3 p-4 hover:border-zinc-300", !n.read_at && "border-brand/40 bg-orange-50/40")}>
                <span className={cn("mt-1.5 size-2 shrink-0 rounded-full", n.read_at ? "bg-transparent" : "bg-brand")} />
                <span className="min-w-0 flex-1">
                  <span className="block text-sm font-medium">{n.title}</span>
                  {n.body && <span className="mt-0.5 block text-sm text-muted">{n.body}</span>}
                  <span className="mt-1 block text-xs text-muted">{timeAgo(n.created_at)}</span>
                </span>
              </Card>
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

function Preferences() {
  const qc = useQueryClient();
  const prefs = useQuery({
    queryKey: ["notification-preferences"],
    queryFn: () => unwrap(api.GET("/v1/me/notifications")),
  });
  const [inThisBrowser, setInThisBrowser] = useState<boolean | null>(null);
  useEffect(() => {
    void currentSubscription().then((s) => setInThisBrowser(!!s));
  }, []);

  const email = useMutation({
    mutationFn: (v: boolean) => unwrap(api.PUT("/v1/me/notifications", { body: { email: v } })),
    onSuccess: (p) => qc.setQueryData(["notification-preferences"], p),
  });
  const push = useMutation({
    mutationFn: async (on: boolean) => {
      if (on) await enablePush(prefs.data!.push_public_key!);
      else await disablePush();
      return on;
    },
    onSuccess: (on) => {
      setInThisBrowser(on);
      void qc.invalidateQueries({ queryKey: ["notification-preferences"] });
    },
  });

  if (!prefs.data) return null;
  const p = prefs.data;
  return (
    <Card className="flex flex-col gap-3 p-4 text-sm">
      <label className="flex items-center gap-3">
        <Mail className="size-4 text-muted" />
        <span className="flex-1">Receber por e-mail</span>
        <input
          type="checkbox"
          className="size-4"
          checked={p.email}
          disabled={email.isPending}
          onChange={(e) => email.mutate(e.target.checked)}
        />
      </label>
      <div className="flex items-center gap-3">
        {inThisBrowser ? <Bell className="size-4 text-muted" /> : <BellOff className="size-4 text-muted" />}
        <span className="flex-1">
          Notificações neste navegador
          {!p.push_public_key && <span className="block text-xs text-muted">Indisponíveis neste servidor.</span>}
          {p.push_public_key && !pushSupported() && (
            <span className="block text-xs text-muted">
              Este navegador não recebe notificações. No iPhone, instale o app na tela inicial.
            </span>
          )}
        </span>
        {p.push_public_key && pushSupported() && inThisBrowser !== null && (
          <Button
            size="sm"
            variant={inThisBrowser ? "secondary" : "primary"}
            disabled={push.isPending}
            onClick={() => push.mutate(!inThisBrowser)}
          >
            {inThisBrowser ? "Desativar" : "Ativar"}
          </Button>
        )}
      </div>
      {(email.error || push.error) && <p className="text-red-700">{(email.error ?? push.error)?.message}</p>}
    </Card>
  );
}
