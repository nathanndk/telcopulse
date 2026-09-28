"use client";

import { useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { incidents, type Incident } from "@/lib/incidents";
import { APIError } from "@/lib/api";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { NativeSelect, NativeSelectOption } from "./ui/native-select";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "./ui/dialog";

const severityOrder: Incident["severity"][] = ["SEV-1", "SEV-2", "SEV-3", "SEV-4"];

export function IncidentEscalation({ incident }: { incident: Incident }) {
  const [open, setOpen] = useState(false);
  const [team, setTeam] = useState("");
  const [owner, setOwner] = useState("");
  const [severity, setSeverity] = useState<Incident["severity"]>(incident.severity);
  const [reason, setReason] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const client = useQueryClient();

  if (incident.state === "Resolved" || incident.state === "Postmortem") return null;

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSaving(true);
    setError("");
    try {
      await incidents.escalate(incident, {
        team: team.trim(),
        owner: owner.trim(),
        severity,
        reason: reason.trim(),
      });
      await client.invalidateQueries({ queryKey: ["incident", incident.id] });
      await client.invalidateQueries({ queryKey: ["incidents"] });
      setOpen(false);
      setTeam("");
      setOwner("");
      setReason("");
      toast.success("Escalation recorded in the incident audit");
    } catch (cause) {
      setError(cause instanceof APIError && cause.status === 409
        ? "This incident changed. Refresh it and review the latest revision before escalating."
        : cause instanceof Error ? cause.message : "Unable to record escalation");
    } finally {
      setSaving(false);
    }
  };

  return <>
    <Button variant="outline" onClick={() => { setSeverity(incident.severity); setError(""); setOpen(true); }}>Escalate</Button>
    <Dialog open={open} onOpenChange={(value) => { if (!saving) setOpen(value); }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Escalate incident</DialogTitle>
          <DialogDescription>Record a coordination handoff to an owning team. This does not send an external on-call notification.</DialogDescription>
        </DialogHeader>
        <form onSubmit={(event) => void submit(event)} className="incident-edit-form">
          <p className="muted">Revision {incident.version} · current severity {incident.severity} · escalation level {incident.escalation_level}</p>
          <div><Label htmlFor="escalation-team">Owning team</Label><Input id="escalation-team" value={team} onChange={(event) => setTeam(event.target.value)} minLength={1} maxLength={100} required disabled={saving} /></div>
          <div><Label htmlFor="escalation-owner">Assign owner (optional)</Label><Input id="escalation-owner" value={owner} onChange={(event) => setOwner(event.target.value)} maxLength={100} disabled={saving} /><p className="muted">Leave blank to keep the current owner.</p></div>
          <div><Label htmlFor="escalation-severity">Severity</Label><NativeSelect id="escalation-severity" value={severity} onChange={(event) => setSeverity(event.target.value as Incident["severity"])} disabled={saving}>{severityOrder.filter((value) => severityOrder.indexOf(value) <= severityOrder.indexOf(incident.severity)).map((value) => <NativeSelectOption key={value} value={value}>{value}</NativeSelectOption>)}</NativeSelect></div>
          <div><Label htmlFor="escalation-reason">Escalation reason</Label><textarea id="escalation-reason" value={reason} onChange={(event) => setReason(event.target.value)} rows={4} maxLength={2000} required disabled={saving} /></div>
          {error && <p role="alert" className="text-destructive">{error}</p>}
          <div className="page-actions"><Button type="button" variant="outline" onClick={() => setOpen(false)} disabled={saving}>Cancel</Button><Button type="submit" disabled={saving}>{saving ? "Recording…" : "Record escalation"}</Button></div>
        </form>
      </DialogContent>
    </Dialog>
  </>;
}
