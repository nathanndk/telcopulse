"use client";

import { useRef, useState } from "react";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { deadLetters, type DeadLetterReason, type DeadLetterRecord } from "@/lib/dead-letters";
import { APIError, timestamp } from "@/lib/api";
import { usePermission } from "./providers";
import { EmptyState, ErrorState, LoadingState, Panel } from "./common";
import { Badge } from "./ui/badge";
import { Button } from "./ui/button";
import { NativeSelect, NativeSelectOption } from "./ui/native-select";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "./ui/dialog";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "./ui/table";

function DeadLetterHistory({ item }: { item: DeadLetterRecord }) {
  const [open, setOpen] = useState(false);
  const [cursors, setCursors] = useState([""]);
  const cursor = cursors[cursors.length - 1];
  const query = useQuery({
    queryKey: ["notification-dead-letter-replays", item.source_partition, item.source_offset, cursor],
    queryFn: () => deadLetters.history(item.source_partition, item.source_offset, cursor),
    enabled: open,
    refetchOnWindowFocus: false,
  });
  return <>
    <Button variant="outline" size="sm" onClick={() => setOpen(true)}>History</Button>
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className="max-h-[min(760px,calc(100dvh-32px))] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Reprocessing history</DialogTitle>
          <DialogDescription>Append-only operator outcomes for partition {item.source_partition}, offset {item.source_offset}. Original Kafka bytes and retry keys remain restricted.</DialogDescription>
        </DialogHeader>
        <div className="flex justify-end"><Button variant="outline" size="sm" onClick={() => void query.refetch()}>Refresh history</Button></div>
        {query.isPending ? <LoadingState /> : query.isError ? <ErrorState error={query.error} retry={() => void query.refetch()} /> : query.data.items.length === 0 ? <p className="muted text-sm">No reprocessing attempts are recorded for this event.</p> : <>
          <ol className="divide-y" aria-label="Reprocessing attempts">{query.data.items.map((attempt, index) => <li key={`${attempt.attempted_at}:${attempt.actor}:${index}`} className="py-3 text-sm">
            <div className="flex items-center justify-between gap-3"><strong>{attempt.status.replaceAll("_", " ")}</strong><span className="muted text-xs">{timestamp(attempt.attempted_at)} WIB</span></div>
            <p className="muted mt-1">{attempt.actor}</p>
            {attempt.status === "REJECTED" ? <p className="mt-1">{attempt.reason}</p> : <Link className="text-link mono mt-1 block" href={`/transactions/${encodeURIComponent(attempt.transaction_id)}`} onClick={() => setOpen(false)}>{attempt.transaction_id}</Link>}
          </li>)}</ol>
          <div className="flex items-center justify-between gap-3 text-xs"><span>Page {cursors.length}</span><div className="flex gap-2"><Button variant="outline" size="sm" disabled={cursors.length === 1} onClick={() => setCursors(values => values.slice(0, -1))}>Previous</Button><Button variant="outline" size="sm" disabled={!query.data.more || !query.data.next} onClick={() => { if (query.data.next) setCursors(values => [...values, query.data.next!]); }}>Next</Button></div></div>
        </>}
      </DialogContent>
    </Dialog>
  </>;
}

function DeadLetterReplay({ item, onDone }: { item: DeadLetterRecord; onDone: () => void }) {
  const allowed = usePermission("replayDeadLetter");
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [retrySame, setRetrySame] = useState(false);
  const [result, setResult] = useState<Awaited<ReturnType<typeof deadLetters.replay>> | null>(null);
  const attempt = useRef<{ source: string; key: string } | null>(null);
  if (!allowed) return null;
  const source = `${item.source_partition}:${item.source_offset}`;

  async function replay() {
    if (busy) return;
    const current = attempt.current?.source === source
      ? attempt.current : { source, key: crypto.randomUUID() };
    attempt.current = current;
    setBusy(true);
    setError("");
    setRetrySame(false);
    try {
      const outcome = await deadLetters.replay(item.source_partition, item.source_offset, current.key);
      attempt.current = null;
      setResult(outcome);
      onDone();
    } catch (cause) {
      if (cause instanceof APIError && [400, 403, 404, 409, 422].includes(cause.status)) attempt.current = null;
      setRetrySame(attempt.current !== null);
      setError(cause instanceof Error ? cause.message : "Replay could not be confirmed.");
    } finally {
      setBusy(false);
    }
  }

  return <>
    <Button variant="outline" size="sm" onClick={() => { setResult(null); setError(""); setOpen(true); }}>Reprocess</Button>
    <Dialog open={open} onOpenChange={(value) => { if (!busy) setOpen(value); }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Reprocess retained event?</DialogTitle>
          <DialogDescription>
            This validates the original quarantined bytes under the current consumer code. A valid event can create one synthetic notification receipt; an event that is still invalid is rejected and audited. The source record stays in quarantine. No new customer purchase is started.
          </DialogDescription>
        </DialogHeader>
        <p className="mono break-all text-sm">{item.source_topic} · partition {item.source_partition} · offset {item.source_offset}</p>
        {result && <div role="status" className="text-sm">
          <strong>{result.status === "DELIVERED" ? "Notification receipt created" : result.status === "ALREADY_DELIVERED" ? "Receipt already existed" : "Event still rejected"}</strong>
          <p>{result.status === "REJECTED" ? result.reason : <Link className="text-link mono" href={`/transactions/${encodeURIComponent(result.transaction_id)}`}>{result.transaction_id}</Link>}</p>
          <p className="muted">Recorded {timestamp(result.attempted_at)} WIB by {result.actor}.</p>
        </div>}
        {error && <p role="alert" className="error-text text-sm">{error}{retrySame && " Retry here to confirm the same command."}</p>}
        <div className="flex justify-end gap-2">
          <Button variant="outline" disabled={busy} onClick={() => setOpen(false)}>Close</Button>
          <Button disabled={busy} onClick={replay}>{busy ? "Reprocessing…" : result ? "Reprocess again" : "Confirm reprocessing"}</Button>
        </div>
      </DialogContent>
    </Dialog>
  </>;
}

export function DeadLetterRegister() {
  const [reason, setReason] = useState<DeadLetterReason>("");
  const [cursors, setCursors] = useState([""]);
  const cursor = cursors[cursors.length - 1];
  const query = useQuery({
    queryKey: ["notification-dead-letters", reason, cursor],
    queryFn: () => deadLetters.list(reason, cursor),
    refetchOnWindowFocus: false,
  });

  return <Panel title="Kafka dead-letter register" description="Poison purchase events retained by notification-service · shared runtime across development and staging · newest first.">
    <div className="table-toolbar">
      <NativeSelect aria-label="Dead-letter reason" value={reason} onChange={(event) => { setReason(event.target.value as DeadLetterReason); setCursors([""]); }}>
        <NativeSelectOption value="">All reasons</NativeSelectOption>
        <NativeSelectOption value="invalid purchase event">Invalid purchase event</NativeSelectOption>
        <NativeSelectOption value="event identity conflict">Event identity conflict</NativeSelectOption>
      </NativeSelect>
      <Button variant="outline" size="sm" onClick={() => void query.refetch()}>Refresh</Button>
      <span className="muted text-sm">Raw payloads are restricted to the quarantine database.</span>
    </div>
    {query.isPending ? <LoadingState /> : query.isError ? <div className="panel-padding"><ErrorState error={query.error} retry={() => void query.refetch()} /></div> : query.data.items.length === 0 ?
      <EmptyState title="No quarantined purchase events" description={reason ? "Try another reason or refresh after a new event." : "Invalid Kafka records will appear here after the consumer retains them."} /> : <>
        <p className="muted px-4 py-2 text-xs sm:hidden">Scroll sideways for reason, digest and publication status.</p>
        <div className="overflow-x-auto"><Table aria-label="Kafka dead-letter records"><TableHeader><TableRow>
          <TableHead>Retained (WIB)</TableHead><TableHead>Source coordinates</TableHead><TableHead>Reason</TableHead><TableHead>Bytes</TableHead><TableHead>SHA-256</TableHead><TableHead>Topic fact</TableHead><TableHead>Last replay</TableHead><TableHead>Action</TableHead>
        </TableRow></TableHeader><TableBody>{query.data.items.map((item) => <TableRow key={`${item.source_topic}:${item.source_partition}:${item.source_offset}`}>
          <TableCell className="nowrap">{timestamp(item.created_at)}</TableCell>
          <TableCell className="mono text-xs"><span className="block">{item.source_topic}</span><span className="muted">partition {item.source_partition} · offset {item.source_offset}</span></TableCell>
          <TableCell>{item.reason}</TableCell>
          <TableCell className="mono">{item.payload_bytes}</TableCell>
          <TableCell className="mono text-xs"><span title={item.payload_sha256}>{item.payload_sha256.slice(0, 16)}…</span></TableCell>
          <TableCell><Badge variant="outline" className={item.publication === "PUBLISHED" ? "status-success" : "status-neutral"}>{item.publication === "PUBLISHED" ? "Published" : item.publication === "PENDING" ? "Pending" : "Not tracked"}</Badge></TableCell>
          <TableCell>{item.replay_status ? <div className="table-stack"><strong>{item.replay_status.replaceAll("_", " ")}</strong><small>{item.replayed_at ? timestamp(item.replayed_at) : ""} · {item.replay_actor}</small></div> : "No attempts"}</TableCell>
          <TableCell><div className="flex gap-2"><DeadLetterHistory item={item} /><DeadLetterReplay item={item} onDone={() => void query.refetch()} /></div></TableCell>
        </TableRow>)}</TableBody></Table></div>
        <div className="pagination"><span>Page {cursors.length} · {query.data.items.length} records shown</span><div>
          <Button size="sm" variant="outline" disabled={cursors.length === 1} onClick={() => setCursors((values) => values.slice(0, -1))}>Previous</Button>
          <Button size="sm" variant="outline" disabled={!query.data.more || !query.data.next} onClick={() => { const next = query.data.next; if (next) setCursors((values) => [...values, next]); }}>Next</Button>
        </div></div>
      </>}
  </Panel>;
}
