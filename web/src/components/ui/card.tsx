import type { HTMLAttributes } from "react";
import { cn } from "@/lib/utils";

export function Card({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("rounded-xl border border-borda bg-white", className)} {...props} />;
}

export function Badge({ className, ...props }: HTMLAttributes<HTMLSpanElement>) {
  return (
    <span
      className={cn("inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium", className)}
      {...props}
    />
  );
}

export function Aviso({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div role="alert" className={cn("rounded-lg border px-4 py-3 text-sm", className)} {...props} />;
}
