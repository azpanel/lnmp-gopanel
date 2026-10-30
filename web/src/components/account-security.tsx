import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { KeyRound } from "lucide-react";
import { lazy, Suspense, useState } from "react";
const SecurityContents = lazy(
  () => import("@/components/account-security-contents"),
);
export function AccountSecurity({ username }: { username: string }) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!busy) setOpen(value);
      }}
    >
      <DialogTrigger asChild>
        <Button variant="ghost" size="icon" aria-label="账户安全">
          <KeyRound size={18} />
        </Button>
      </DialogTrigger>
      <DialogContent
        className="account-security-dialog"
        showCloseButton={!busy}
        onEscapeKeyDown={(event) => {
          if (busy) event.preventDefault();
        }}
        onInteractOutside={(event) => {
          if (busy) event.preventDefault();
        }}
      >
        <DialogHeader>
          <DialogTitle>账户安全</DialogTitle>
          <DialogDescription>管理登录密码和 TOTP 双重验证。</DialogDescription>
        </DialogHeader>
        {open && (
          <Suspense fallback={<p role="status">正在加载账户设置…</p>}>
            <SecurityContents
              username={username}
              busy={busy}
              setBusy={setBusy}
            />
          </Suspense>
        )}
      </DialogContent>
    </Dialog>
  );
}
