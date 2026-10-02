import { cva, type VariantProps } from "class-variance-authority";
import type { ButtonHTMLAttributes } from "react";
import { cn } from "@/lib/utils";

const variantes = cva(
  "inline-flex items-center justify-center gap-2 rounded-lg text-sm font-medium transition-colors disabled:pointer-events-none disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-marca",
  {
    variants: {
      variante: {
        primario: "bg-marca text-white hover:bg-marca-escura",
        secundario: "border border-borda bg-white hover:bg-zinc-50",
        fantasma: "hover:bg-zinc-100",
        perigo: "border border-red-200 bg-white text-red-700 hover:bg-red-50",
      },
      tamanho: { md: "h-10 px-4", sm: "h-8 px-3" },
    },
    defaultVariants: { variante: "primario", tamanho: "md" },
  },
);

export function Button({
  className,
  variante,
  tamanho,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & VariantProps<typeof variantes>) {
  return <button className={cn(variantes({ variante, tamanho }), className)} {...props} />;
}
