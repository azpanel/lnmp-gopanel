import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

export function Progress({
  value,
  active = false,
  failed = false,
  className,
  ...props
}: ComponentProps<"div"> & {
  value?: number;
  active?: boolean;
  failed?: boolean;
}) {
  return (
    <div
      {...props}
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={value}
      data-active={active}
      data-failed={failed}
      className={cn("backup-progress-track", className)}
    >
      <div
        className="backup-progress-fill"
        data-indeterminate={value == null}
        style={{ width: value == null ? "35%" : `${value}%` }}
      />
    </div>
  );
}
