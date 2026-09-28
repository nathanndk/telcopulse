"use client";

import { useQuery } from "@tanstack/react-query";
import { api, timestamp } from "@/lib/api";
import { Panel } from "./common";
import { Button } from "./ui/button";

export function TransactionNotification({ id }: { id: string }) {
  const query = useQuery({
    queryKey: ["transaction-notification", id],
    queryFn: () => api.notificationStatus(id),
    refetchInterval: (state) => state.state.data?.status === "DELIVERED" ? false : 5000,
  });

  return <Panel title="Synthetic notification" description="Receipt status from notification-service after Kafka consumption.">
    <div className="p-5" aria-live="polite">
      {query.isPending ? <p>Checking durable notification receipt…</p> : query.isError ? <>
        <p role="alert" className="error-text">Notification status is unavailable. Delivery has not been ruled out.</p>
        <Button variant="outline" size="sm" className="mt-3" onClick={() => void query.refetch()}>Try again</Button>
      </> : query.data.status === "DELIVERED" ? <>
        <strong>Delivered</strong>
        <p className="muted text-sm">Synthetic receipt recorded {query.data.delivered_at ? timestamp(query.data.delivered_at) : ""} WIB. One logical receipt is retained across Kafka retries.</p>
      </> : query.data.status === "PROCESSING" ? <>
        <strong>Purchase processing</strong>
        <p className="muted text-sm">The purchase has not reached its terminal notification enqueue. Checking again automatically.</p>
      </> : <>
        <strong>Awaiting delivery</strong>
        <p className="muted text-sm">The purchase event was queued, but notification-service has no durable receipt yet. Kafka or consumer delay may be involved; checking again automatically.</p>
      </>}
    </div>
  </Panel>;
}
