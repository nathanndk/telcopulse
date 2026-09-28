"use client";
import type {
  UseFieldArrayReturn,
  UseFormRegister,
  FieldErrors,
} from "react-hook-form";
import type { IncidentEditValues } from "./incident-editor";
import { actionPriorities, actionStatuses } from "@/lib/incidents";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Label } from "./ui/label";
import { NativeSelect, NativeSelectOption } from "./ui/native-select";
export function IncidentCollections({
  evidence,
  actions,
  register,
  errors,
  appendOnlyEvidence,
  existingEvidenceCount,
  allowActions,
}: {
  evidence: UseFieldArrayReturn<IncidentEditValues, "evidence">;
  actions: UseFieldArrayReturn<IncidentEditValues, "action_items">;
  register: UseFormRegister<IncidentEditValues>;
  errors: FieldErrors<IncidentEditValues>;
  appendOnlyEvidence: boolean;
  existingEvidenceCount: number;
  allowActions: boolean;
}) {
  return (
    <>
      <section className="incident-edit-fields" aria-label="Edit evidence">
        <h3>Evidence</h3>
        <p className="muted">
          Changes take effect when you save the incident. Up to 50 references.
        </p>
        {evidence.fields.map((item, index) => appendOnlyEvidence && index < existingEvidenceCount ? (
          <div className="incident-collection-entry" key={item.id}>
            <strong>Evidence {index + 1}</strong>
            <p>{item.kind} · {item.summary}</p>
            {item.url && <p className="muted">{item.url}</p>}
          </div>
        ) : (
          <fieldset className="incident-collection-entry" key={item.id}>
            <legend>Evidence {index + 1}</legend>
            <Label htmlFor={`e-kind-${item.id}`}>Evidence kind</Label>
            <NativeSelect
              id={`e-kind-${item.id}`}
              {...register(`evidence.${index}.kind`)}
            >
              {["metric", "log", "trace", "deployment", "note"].map((kind) => (
                <NativeSelectOption key={kind} value={kind}>
                  {kind}
                </NativeSelectOption>
              ))}
            </NativeSelect>
            <Label htmlFor={`e-summary-${item.id}`}>Evidence summary</Label>
            <Input
              id={`e-summary-${item.id}`}
              {...register(`evidence.${index}.summary`)}
            />
            <Label htmlFor={`e-url-${item.id}`}>Evidence URL (optional)</Label>
            <Input
              id={`e-url-${item.id}`}
              {...register(`evidence.${index}.url`)}
            />
            {errors.evidence?.[index] && (
              <p role="alert">
                {errors.evidence[index]?.summary?.message ||
                  errors.evidence[index]?.url?.message ||
                  "Choose a valid evidence kind"}
              </p>
            )}
            <Button
              type="button"
              variant="outline"
              onClick={() => evidence.remove(index)}
            >
              Remove evidence {index + 1}
            </Button>
          </fieldset>
        ))}
        <Button
          type="button"
          variant="outline"
          disabled={evidence.fields.length >= 50}
          onClick={() =>
            evidence.append({ kind: "note", summary: "", url: "" })
          }
        >
          Add evidence
        </Button>
      </section>
      {allowActions && <section className="incident-edit-fields" aria-label="Edit action items">
        <h3>Action items</h3>
        {actions.fields.map((item, index) => (
          <fieldset className="incident-collection-entry" key={item.id}>
            <legend>Action {index + 1}</legend>
            <Label htmlFor={`a-title-${item.id}`}>Action title</Label>
            <Input
              id={`a-title-${item.id}`}
              {...register(`action_items.${index}.title`)}
            />
            <Label htmlFor={`a-owner-${item.id}`}>Action owner</Label>
            <Input
              id={`a-owner-${item.id}`}
              {...register(`action_items.${index}.owner`)}
            />
            <Label htmlFor={`a-priority-${item.id}`}>Action priority</Label>
            <NativeSelect id={`a-priority-${item.id}`} {...register(`action_items.${index}.priority`)}>
              {actionPriorities.map((priority) => <NativeSelectOption key={priority} value={priority}>{priority}</NativeSelectOption>)}
            </NativeSelect>
            <Label htmlFor={`a-status-${item.id}`}>Action status</Label>
            <NativeSelect id={`a-status-${item.id}`} {...register(`action_items.${index}.status`)}>
              {actionStatuses.map((status) => <NativeSelectOption key={status} value={status}>{status}</NativeSelectOption>)}
            </NativeSelect>
            <Label htmlFor={`a-due-${item.id}`}>
              Due timestamp (ISO 8601, optional)
            </Label>
            <Input
              id={`a-due-${item.id}`}
              placeholder="2026-10-01T09:00:00+07:00"
              {...register(`action_items.${index}.due_at`, {
                setValueAs: (v) => (v === "" ? undefined : v),
              })}
            />
            {errors.action_items?.[index] && (
              <p role="alert">
                {errors.action_items[index]?.title?.message ||
                  errors.action_items[index]?.owner?.message ||
                  errors.action_items[index]?.due_at?.message}
              </p>
            )}
            <Button
              type="button"
              variant="outline"
              onClick={() => actions.remove(index)}
            >
              Remove action {index + 1}
            </Button>
          </fieldset>
        ))}
        <Button
          type="button"
          variant="outline"
          disabled={actions.fields.length >= 50}
          onClick={() => actions.append({ title: "", owner: "", done: false, priority: "P2", status: "Open" })}
        >
          Add action item
        </Button>
      </section>}
    </>
  );
}
