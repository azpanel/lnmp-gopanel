import { useEffect, useRef } from "react";
import { Button } from "@/components/ui/button";

export function ConsequenceConfirmation({
  title,
  description,
  confirmLabel,
  busy,
  onCancel,
  onConfirm,
}: {
  title: string;
  description: string;
  confirmLabel: string;
  busy: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const cancel = useRef<HTMLButtonElement>(null);
  const submitted = useRef(false);
  useEffect(() => {
    cancel.current?.focus();
  }, []);
  return (
    <div className="notice space-y-3" role="group" aria-label={title}>
      <strong>{title}</strong>
      <p>{description}</p>
      <div className="flex flex-wrap justify-end gap-2">
        <Button
          ref={cancel}
          type="button"
          variant="outline"
          disabled={busy}
          onClick={() => {
            if (!submitted.current) onCancel();
          }}
        >
          取消操作
        </Button>
        <Button
          type="button"
          variant="destructive"
          disabled={busy}
          onClick={() => {
            if (submitted.current || busy) return;
            submitted.current = true;
            onConfirm();
          }}
        >
          {busy ? "正在执行…" : confirmLabel}
        </Button>
      </div>
    </div>
  );
}
