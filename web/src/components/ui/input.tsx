import type { InputHTMLAttributes, SelectHTMLAttributes } from "react";
import { cn } from "@/lib/utils";

const base =
  "h-10 w-full rounded-lg border border-borda bg-white px-3 text-sm outline-none placeholder:text-suave focus:border-marca focus:ring-2 focus:ring-marca/20";

export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={cn(base, className)} {...props} />;
}

export function Select({ className, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select className={cn(base, "pr-8", className)} {...props} />;
}

export function Rotulo({ children, texto }: { children: React.ReactNode; texto: string }) {
  return (
    <label className="flex flex-col gap-1 text-xs font-medium text-suave">
      {texto}
      {children}
    </label>
  );
}
