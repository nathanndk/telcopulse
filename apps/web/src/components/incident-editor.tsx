"use client";
import { IncidentCollections } from "./incident-collections";
import { useState } from "react";
import { useForm, useFieldArray, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { incidents, incidentStates, actionPriorities, actionStatuses, type Incident } from "@/lib/incidents";
import { APIError } from "@/lib/api";
import { usePermission, useSession } from "./providers";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { NativeSelect, NativeSelectOption } from "./ui/native-select";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "./ui/dialog";
const transitions: Record<Incident["state"], Incident["state"][]> = {
  Detected: ["Acknowledged"],
  Acknowledged: ["Investigating"],
  Investigating: ["Identified"],
  Identified: ["Mitigating", "Investigating"],
  Mitigating: ["Monitoring", "Investigating"],
  Monitoring: ["Resolved", "Mitigating", "Investigating"],
  Resolved: ["Postmortem", "Investigating"],
  Postmortem: [],
};
const schema = z
  .object({
    title: z.string().trim().min(3).max(200),
    severity: z.enum(["SEV-1", "SEV-2", "SEV-3", "SEV-4"]),
    owner: z.string().trim().max(100),
    state: z.enum(incidentStates),
    impact: z.string().max(4000),
    evidence: z
      .array(
        z.object({
          kind: z
            .string()
            .refine((v) =>
              ["metric", "log", "trace", "deployment", "note"].includes(v),
            ),
          summary: z.string().trim().min(1).max(1000),
          url: z
            .string()
            .max(2048)
            .optional()
            .refine((v) => {
              if (!v) return true;
              try {
                const u = new URL(v);
                return (
                  ["http:", "https:"].includes(u.protocol) &&
                  !u.username &&
                  !u.password
                );
              } catch {
                return false;
              }
            }, "Use an HTTP(S) URL without credentials"),
        }),
      )
      .max(50),
    action_items: z
      .array(
        z.object({
          title: z.string().trim().min(1).max(500),
          owner: z
            .string()
            .trim()
            .min(1, "An action owner is required")
            .max(100),
          done: z.boolean(),
          priority: z.enum(actionPriorities),
          status: z.enum(actionStatuses),
          due_at: z.string().datetime({ offset: true }).optional(),
        }),
      )
      .max(50),
    related_deployment: z.string().max(200),
    affected_users: z
      .number()
      .int()
      .min(0)
      .max(Number.MAX_SAFE_INTEGER)
      .nullable(),
    affected_transactions: z
      .number()
      .int()
      .min(0)
      .max(Number.MAX_SAFE_INTEGER)
      .nullable(),
    error_rate: z.number().min(0).max(1).nullable(),
    success_rate: z.number().min(0).max(1).nullable(),
    latency_ms: z.number().min(0).nullable(),
    root_cause: z.string().max(4000),
    mitigation: z.string().max(4000),
    resolution: z.string().max(4000),
    recovery_observation: z.string().max(1000),
    recovery_source_url: z.string().max(2048),
    recovery_observed_at: z.string(),
    postmortem_notes: z.string().max(4000),
    note: z
      .string()
      .trim()
      .min(1, "Explain this change for the audit history")
      .max(2000),
  })
  .superRefine((v, ctx) => {
    const required: Array<[keyof typeof v, boolean]> = [
      ["owner", v.state !== "Detected"],
      [
        "root_cause",
        [
          "Identified",
          "Mitigating",
          "Monitoring",
          "Resolved",
          "Postmortem",
        ].includes(v.state),
      ],
      [
        "mitigation",
        ["Mitigating", "Monitoring", "Resolved", "Postmortem"].includes(
          v.state,
        ),
      ],
      ["resolution", ["Resolved", "Postmortem"].includes(v.state)],
      ["postmortem_notes", v.state === "Postmortem"],
    ];
    for (const [key, needed] of required)
      if (needed && typeof v[key] === "string" && !(v[key] as string).trim())
        ctx.addIssue({
          code: "custom",
          path: [key],
          message: `Required for ${v.state}`,
        });
  });
export type IncidentEditValues = z.infer<typeof schema>;
type Values = IncidentEditValues;
export function IncidentEditor({
  incident,
  metricEvidence,
  disabled = false,
}: {
  incident: Incident;
  disabled?: boolean;
  metricEvidence?: { kind: string; summary: string };
}) {
  const canEdit = usePermission("editIncident");
  const { session } = useSession();
  const operator = session.kind === "authenticated" && session.user.role === "Operator";
  const [snapshot, setSnapshot] = useState<Incident | null>(null);
  if (!canEdit || (operator && (incident.state === "Resolved" || incident.state === "Postmortem"))) return null;
  return (
    <>
      <Button
        disabled={
          disabled ||
          (!!metricEvidence && (incident.evidence?.length ?? 0) >= 50)
        }
        onClick={() =>
          setSnapshot(
            metricEvidence
              ? {
                  ...incident,
                  evidence: [...(incident.evidence ?? []), metricEvidence],
                }
              : incident,
          )
        }
      >
        {metricEvidence ? "Review metric evidence" : "Edit incident"}
      </Button>
      <Dialog
        open={snapshot !== null}
        onOpenChange={(open) => {
          if (!open) setSnapshot(null);
        }}
      >
        <DialogContent className="incident-editor">
          <DialogHeader>
            <DialogTitle>Edit incident</DialogTitle>
            <DialogDescription>
              Changes are recorded under {session.kind === "authenticated" ? session.user.username : "local-operator"}. Review the state and
              include an audit note.
            </DialogDescription>
          </DialogHeader>
          {snapshot && (
            <EditorForm
              key={snapshot.version}
              current={snapshot}
              close={() => setSnapshot(null)}
              reload={setSnapshot}
            />
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
function EditorForm({
  current,
  close,
  reload,
}: {
  current: Incident;
  close: () => void;
  reload: (i: Incident) => void;
}) {
  const client = useQueryClient();
  const { session } = useSession();
  const operator = session.kind === "authenticated" && session.user.role === "Operator";
  const [error, setError] = useState("");
  const [conflict, setConflict] = useState(false);
  const [loading, setLoading] = useState(false);
  const {
    register,
    control,
    handleSubmit,
    setError: setFieldError,
    formState: { errors, isSubmitting },
  } = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      ...current,
      evidence: current.evidence ?? [],
      action_items: current.action_items ?? [],
      recovery_observation: "",
      recovery_source_url: "",
      recovery_observed_at: "",
      note: "",
    },
  });
  const evidence = useFieldArray({ control, name: "evidence" });
  const actions = useFieldArray({ control, name: "action_items" });
  const targetState = useWatch({ control, name: "state" });
  const submit = handleSubmit(async (values) => {
    setError("");
    const resolving = current.state === "Monitoring" && values.state === "Resolved";
    if (resolving) {
      if (values.recovery_observation.trim().length < 10) {
        setFieldError("recovery_observation", { message: "Describe the measured recovery in at least 10 characters" });
        return;
      }
      try {
        const source = new URL(values.recovery_source_url);
        if (!["http:", "https:"].includes(source.protocol) || source.username || source.password) throw new Error("invalid source");
      } catch {
        setFieldError("recovery_source_url", { message: "Link to an HTTP(S) evidence source without credentials" });
        return;
      }
      if (!values.recovery_observed_at || Number.isNaN(Date.parse(values.recovery_observed_at))) {
        setFieldError("recovery_observed_at", { message: "Record when recovery was observed" });
        return;
      }
    }
    const { recovery_observation, recovery_source_url, recovery_observed_at, ...editable } = values;
    const validation = resolving ? { recovery_validation: {
      observation: recovery_observation.trim(),
      source_url: recovery_source_url.trim(),
      observed_at: new Date(recovery_observed_at).toISOString(),
    } } : {};
    try {
      await incidents.update(current, operator ? {
        ...editable,
        ...validation,
        title: current.title,
        severity: current.severity,
        resolution: current.resolution,
        postmortem_notes: current.postmortem_notes,
        action_items: current.action_items ?? [],
      } : {
        ...editable,
        ...validation,
        action_items: values.action_items.map((item) => ({
          ...item,
          done: item.status === "Completed",
        })),
      });
      await client.invalidateQueries({ queryKey: ["incident", current.id] });
      await client.invalidateQueries({ queryKey: ["incidents"] });
      toast.success("Incident updated and audit entry recorded");
      close();
    } catch (e) {
      setConflict(e instanceof APIError && e.status === 409);
      setError(e instanceof Error ? e.message : "Unable to save incident");
    }
  });
  const fields = [
    ["impact", "Impact"],
    ["root_cause", "Root cause"],
    ["mitigation", "Mitigation"],
    ["resolution", "Resolution"],
    ["postmortem_notes", "Postmortem notes"],
    ["note", "Audit note"],
  ] as const;
  const feedback = (key: keyof Values) =>
    errors[key] && (
      <p id={`error-${key}`} className="text-destructive" role="alert">
        {errors[key]?.message}
      </p>
    );
  return (
    <form onSubmit={submit} className="incident-edit-form">
      <p className="muted">
        Editing revision {current.version} · {current.environment} ·{" "}
        {current.service}
      </p>
      <fieldset
        disabled={isSubmitting || loading}
        className="incident-edit-fields"
      >
        {!operator && <div>
          <Label htmlFor="incident-title">Title</Label>
          <Input
            id="incident-title"
            {...register("title")}
            aria-invalid={!!errors.title}
            aria-describedby="error-title"
          />
          {feedback("title")}
        </div>}
        <div className="incident-edit-pair">
          {!operator && <div>
            <Label htmlFor="incident-severity">Severity</Label>
            <NativeSelect id="incident-severity" {...register("severity")}>
              {["SEV-1", "SEV-2", "SEV-3", "SEV-4"].map((v) => (
                <NativeSelectOption key={v} value={v}>
                  {v}
                </NativeSelectOption>
              ))}
            </NativeSelect>
          </div>}
          <div>
            <Label htmlFor="incident-state">State</Label>
            <NativeSelect id="incident-state" {...register("state")}>
              {[current.state, ...transitions[current.state]].filter((v) =>
                (!operator || (v !== "Resolved" && v !== "Postmortem")) &&
                !(v === "Postmortem" && current.state === "Resolved" && (current.severity === "SEV-1" || current.severity === "SEV-2"))
              ).map((v) => (
                <NativeSelectOption key={v} value={v}>
                  {v}
                </NativeSelectOption>
              ))}
            </NativeSelect>
            {current.state === "Resolved" && (current.severity === "SEV-1" || current.severity === "SEV-2") && !operator ?
              <p className="muted">Generate the structured postmortem from incident detail for a major incident.</p> : null}
          </div>
        </div>
        <div>
          <Label htmlFor="incident-owner">Owner</Label>
          <Input
            id="incident-owner"
            {...register("owner")}
            aria-invalid={!!errors.owner}
            aria-describedby="error-owner"
          />
          {feedback("owner")}
        </div>
        <div>
          <Label htmlFor="incident-deployment">Related deployment</Label>
          <Input
            id="incident-deployment"
            {...register("related_deployment")}
            aria-invalid={!!errors.related_deployment}
          />
          {feedback("related_deployment")}
        </div>
        <p className="muted">
          Leave measurements blank when unknown. Rates are fractions from 0 to
          1; for example, 0.05 means 5%.
        </p>
        {(
          [
            ["affected_users", "Affected users"],
            ["affected_transactions", "Affected transactions"],
            ["error_rate", "Error rate (0–1)"],
            ["success_rate", "Success rate (0–1)"],
            ["latency_ms", "Latency (ms)"],
          ] as const
        ).map(([key, label]) => (
          <div key={key}>
            <Label htmlFor={`incident-${key}`}>{label}</Label>
            <Input
              id={`incident-${key}`}
              type="number"
              step={key.startsWith("affected_") ? "1" : "any"}
              {...register(key, {
                setValueAs: (value) =>
                  value === "" || value == null ? null : Number(value),
              })}
              aria-invalid={!!errors[key]}
              aria-describedby={`error-${key}`}
            />
            {feedback(key)}
          </div>
        ))}
        {fields.filter(([key]) => !operator || (key !== "resolution" && key !== "postmortem_notes")).map(([key, label]) => (
          <div key={key}>
            <Label htmlFor={`incident-${key}`}>{label}</Label>
            <textarea
              id={`incident-${key}`}
              rows={3}
              {...register(key)}
              aria-invalid={!!errors[key]}
              aria-describedby={`error-${key}`}
            />
            {feedback(key)}
          </div>
        ))}
        {current.state === "Monitoring" && targetState === "Resolved" && !operator ? <div className="incident-recovery-fields">
          <h3>Recovery validation</h3>
          <p className="muted">Review a current measurement or successful synthetic transaction before resolving. Record what recovered and link the source. Shared-runtime HTTP health alone does not prove business recovery.</p>
          <div>
            <Label htmlFor="incident-recovery-observation">Recovery observation</Label>
            <textarea id="incident-recovery-observation" rows={3} {...register("recovery_observation")} aria-invalid={!!errors.recovery_observation} aria-describedby="error-recovery_observation" />
            {feedback("recovery_observation")}
          </div>
          <div>
            <Label htmlFor="incident-recovery-source">Evidence URL</Label>
            <Input id="incident-recovery-source" type="url" {...register("recovery_source_url")} aria-invalid={!!errors.recovery_source_url} aria-describedby="error-recovery_source_url" />
            {feedback("recovery_source_url")}
          </div>
          <div>
            <Label htmlFor="incident-recovery-time">Observed at</Label>
            <Input id="incident-recovery-time" type="datetime-local" step="1" {...register("recovery_observed_at")} aria-invalid={!!errors.recovery_observed_at} aria-describedby="error-recovery_observed_at" />
            {feedback("recovery_observed_at")}
          </div>
        </div> : null}
        <IncidentCollections
          evidence={evidence}
          actions={actions}
          register={register}
          errors={errors}
          appendOnlyEvidence={operator}
          existingEvidenceCount={current.evidence?.length ?? 0}
          allowActions={!operator}
        />
      </fieldset>
      {error && (
        <div role="alert" className="incident-edit-error">
          <p>
            {conflict
              ? "This incident changed while you were editing. Your draft is preserved. Reload the latest revision to review the other changes before editing again."
              : error}
          </p>
          {conflict && (
            <Button
              type="button"
              variant="outline"
              disabled={loading}
              onClick={async () => {
                setLoading(true);
                try {
                  reload((await incidents.detail(current.id)).incident);
                } catch (e) {
                  setError(e instanceof Error ? e.message : "Reload failed");
                } finally {
                  setLoading(false);
                }
              }}
            >
              Discard draft and reload latest
            </Button>
          )}
        </div>
      )}
      <div className="page-actions">
        <Button
          type="button"
          variant="outline"
          disabled={isSubmitting}
          onClick={close}
        >
          Cancel
        </Button>
        <Button type="submit" disabled={isSubmitting || conflict || loading}>
          {isSubmitting ? "Saving…" : "Save incident"}
        </Button>
      </div>
    </form>
  );
}
