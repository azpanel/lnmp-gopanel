import type { ComponentProps } from "react";
import { X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

export function Chip({ className, ...props }: ComponentProps<"span">) {
  return (
    <Badge
      variant="outline"
      data-slot="chip"
      className={cn(
        "h-7 max-w-full gap-1 bg-muted/40 py-0 pr-0.5 pl-2 text-xs font-normal",
        className,
      )}
      {...props}
    />
  );
}

export function ChipRemove({
  className,
  ...props
}: Omit<ComponentProps<"button">, "children">) {
  return (
    <button
      type="button"
      data-slot="chip-remove"
      className={cn(
        "inline-flex size-6 shrink-0 cursor-pointer items-center justify-center rounded-full text-muted-foreground outline-none hover:bg-destructive/10 hover:text-destructive focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-40",
        className,
      )}
      {...props}
    >
      <X size={12} aria-hidden="true" />
    </button>
  );
}
