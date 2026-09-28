"use client";

import { useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { RotateCcw } from "lucide-react";
import { api, APIError, money } from "@/lib/api";
import type { Transaction } from "@/lib/contracts";
import { usePermission } from "./providers";
import { Button } from "./ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "./ui/dialog";

export function ReplayFailedTransaction({ transaction }: { transaction: Transaction }) {
  const canPurchase = usePermission("runPurchase");
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [retrySame, setRetrySame] = useState(false);
  const attempt = useRef<{ source: string; key: string } | null>(null);

  if (!canPurchase || transaction.status !== "FAILED") return null;

  async function replay() {
    if (busy) return;
    const current = attempt.current?.source === transaction.id
      ? attempt.current
      : { source: transaction.id, key: crypto.randomUUID() };
    attempt.current = current;
    setBusy(true);
    setError("");
    setRetrySame(false);
    try {
      const result = await api.purchase({
        customer_id: transaction.customer_id,
        package_id: transaction.package_id,
        payment_method: transaction.payment_method,
        environment: transaction.environment,
        replay_of: transaction.id,
      }, current.key);
      attempt.current = null;
      setOpen(false);
      router.push(`/transactions/${encodeURIComponent(result.id)}`);
    } catch (cause) {
      if (cause instanceof APIError && [400, 409, 422].includes(cause.status)) attempt.current = null;
      setRetrySame(attempt.current !== null);
      setError(cause instanceof Error ? cause.message : "Replay could not be confirmed.");
    } finally {
      setBusy(false);
    }
  }

  return <>
    <Button variant="outline" size="sm" onClick={() => { setError(""); setRetrySame(false); setOpen(true); }}>
      <RotateCcw /> Replay failed transaction
    </Button>
    <Dialog open={open} onOpenChange={(value) => { if (!busy) setOpen(value); }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Replay failed transaction?</DialogTitle>
          <DialogDescription>
            This starts a new synthetic purchase with the same customer, package and payment method. The failed transaction remains unchanged and both attempts stay linked. Current package price and balance are rechecked; a successful Pulsa replay can debit synthetic balance. An active failure injection may make the new attempt fail again.
          </DialogDescription>
        </DialogHeader>
        <p className="mono break-all text-sm">Source: {transaction.id}</p>
        <p className="text-sm">{transaction.package_name} · {transaction.payment_method} · previous amount {money(transaction.amount_idr)}</p>
        {error && <p role="alert" className="error-text text-sm">{error}{retrySame && " Retry here to confirm the same attempt."}</p>}
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => setOpen(false)} disabled={busy}>Cancel</Button>
          <Button onClick={replay} disabled={busy}>{busy ? "Replaying…" : "Start linked replay"}</Button>
        </div>
      </DialogContent>
    </Dialog>
  </>;
}
