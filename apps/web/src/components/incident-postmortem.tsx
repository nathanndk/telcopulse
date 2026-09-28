"use client";

import { useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { incidents, type Incident, type PostmortemReport } from "@/lib/incidents";
import { APIError, timestamp } from "@/lib/api";
import { usePermission } from "./providers";
import { Panel } from "./common";
import { Button } from "./ui/button";
import { Label } from "./ui/label";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "./ui/dialog";

type Narrative = {
  summary: string;
  detection: string;
  contributing_factors: string;
  what_went_well: string;
  what_went_wrong: string;
  note: string;
};

const prompts: Array<[keyof Narrative, string]> = [
  ["summary", "Summary"],
  ["detection", "Detection"],
  ["contributing_factors", "Contributing factors"],
  ["what_went_well", "What went well"],
  ["what_went_wrong", "What went wrong"],
  ["note", "Audit note"],
];

export function IncidentPostmortem({ incident, report }: { incident: Incident; report?: PostmortemReport }) {
  const canGenerate = usePermission("generatePostmortem");
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [draft, setDraft] = useState<Narrative>({
    summary: incident.postmortem_notes || "",
    detection: "",
    contributing_factors: "",
    what_went_well: "",
    what_went_wrong: "",
    note: "",
  });

  if (incident.state !== "Resolved" && incident.state !== "Postmortem") return null;

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setSaving(true);
    setError("");
    try {
      await incidents.generatePostmortem(incident, Object.fromEntries(
        Object.entries(draft).map(([key, value]) => [key, value.trim()]),
      ) as Narrative);
      await client.invalidateQueries({ queryKey: ["incident", incident.id] });
      await client.invalidateQueries({ queryKey: ["incidents"] });
      setOpen(false);
      toast.success("Structured postmortem generated and audited");
    } catch (cause) {
      setError(cause instanceof APIError && cause.status === 409
        ? "This incident changed or already has a postmortem. Refresh the incident before trying again."
        : cause instanceof Error ? cause.message : "Unable to generate postmortem");
    } finally {
      setSaving(false);
    }
  };

  return <section id="postmortem"><Panel title="Postmortem" description={report ? `Generated at incident revision ${report.incident_version}` : "Create a reviewed learning record from this resolved incident"}>
    <div className="panel-padding incident-notes">
      {report ? <>
        <p className="muted">Generated {timestamp(report.generated_at)} WIB by {report.generated_by}. This report is a frozen source snapshot; later incident edits do not rewrite it.</p>
        <dl>
          {([
            ["Summary", report.summary], ["Impact", report.impact], ["Detection", report.detection],
            ["Root cause", report.root_cause], ["Contributing factors", report.contributing_factors],
            ["Mitigation", report.mitigation ?? "Not included in this older report"],
            ["Resolution", report.resolution],
            ["Recovery validation", report.recovery_validation ? `${report.recovery_validation.observation} · observed ${timestamp(report.recovery_validation.observed_at)} WIB · validated by ${report.recovery_validation.validated_by}` : "Not included in this older report"],
            ["What went well", report.what_went_well],
            ["What went wrong", report.what_went_wrong],
          ] as const).map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}
        </dl>
        {report.recovery_validation && <a href={report.recovery_validation.source_url} target="_blank" rel="noopener noreferrer">Open recovery evidence ↗</a>}
        <h3>Timeline</h3>
        <ol className="incident-timeline">
          {report.timeline.map((event) => <li key={event.version}>
            <strong>Revision {event.version} · {event.state}</strong> · {timestamp(event.at)} WIB · {event.actor}
            <p>{event.note}</p>
          </li>)}
        </ol>
        <h3>Action items at generation</h3>
        {!report.action_items?.length ? <p className="muted">No corrective actions were recorded.</p> : <ul>
          {report.action_items.map((item, index) => <li key={`${item.title}-${index}`}>
            <strong>{item.priority} · {item.status}</strong> — {item.title} · {item.owner}{item.due_at ? ` · due ${timestamp(item.due_at)} WIB` : ""}
          </li>)}
        </ul>}
      </> : <>
        <p className="muted">The incident has no structured postmortem. Generation captures the current impact, root cause, resolution, action items and complete audited timeline.</p>
        {!incident.impact.trim() && <p role="alert">Record the business impact in the incident before generating the report.</p>}
        {canGenerate && <Button variant="outline" disabled={!incident.impact.trim()} onClick={() => { setError(""); setOpen(true); }}>Generate postmortem</Button>}
      </>}
    </div>
    <Dialog open={open} onOpenChange={(value) => { if (!saving) setOpen(value); }}>
      <DialogContent className="incident-editor">
        <DialogHeader>
          <DialogTitle>Generate postmortem</DialogTitle>
          <DialogDescription>Review incident revision {incident.version}. This creates an immutable report and advances the incident to Postmortem.</DialogDescription>
        </DialogHeader>
        <form onSubmit={(event) => void submit(event)} className="incident-edit-form">
          <p className="muted">Impact, RCA, resolution, timeline and action items will be copied from the current audited incident.</p>
          <fieldset disabled={saving} className="incident-edit-fields">
            {prompts.map(([key, label]) => <div key={key}>
              <Label htmlFor={`postmortem-${key}`}>{label}</Label>
              <textarea id={`postmortem-${key}`} value={draft[key]} onChange={(event) => setDraft((current) => ({ ...current, [key]: event.target.value }))} required minLength={1} maxLength={key === "note" ? 2000 : 4000} rows={key === "summary" ? 3 : 2} />
            </div>)}
          </fieldset>
          {error && <p role="alert" className="text-destructive">{error}</p>}
          <div className="page-actions">
            <Button type="button" variant="outline" disabled={saving} onClick={() => setOpen(false)}>Cancel</Button>
            <Button type="submit" disabled={saving}>{saving ? "Generating…" : "Generate and audit"}</Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  </Panel></section>;
}
