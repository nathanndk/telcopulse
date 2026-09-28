"use client";
import { useState } from "react";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { incidents, type IncidentCreation } from "@/lib/incidents";
import { APIError } from "@/lib/api";
import type { Environment } from "@/lib/contracts";
import { useSession } from "./providers";
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
const schema = z.object({
  title: z.string().trim().min(3).max(200),
  severity: z.enum(["SEV-1", "SEV-2", "SEV-3", "SEV-4"]),
  owner: z.string().trim().max(100),
  service: z.string().trim().min(1, "Choose an affected service").max(100),
  impact: z.string().max(4000),
});
type Values = z.infer<typeof schema>;
export function IncidentCreator({ environment }: { environment: Environment }) {
  const { session } = useSession();
  const [open, setOpen] = useState(false);
  const [error, setError] = useState("");
  const [command, setCommand] = useState<{
    input: IncidentCreation;
    key: string;
  } | null>(null);
  const router = useRouter();
  const client = useQueryClient();
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors, isSubmitting },
  } = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      title: "",
      severity: "SEV-3",
      owner: "",
      service: "",
      impact: "",
    },
  });
  const submit = handleSubmit(async (values) => {
    const attempt = command ?? {
      input: { ...values, environment },
      key: crypto.randomUUID(),
    };
    setCommand(attempt);
    setError("");
    try {
      const result = await incidents.create(attempt.input, attempt.key);
      await client.invalidateQueries({ queryKey: ["incidents"] });
      setCommand(null);
      reset();
      setOpen(false);
      toast.success("Incident recorded");
      router.push(`/incidents/${result.id}`);
    } catch (e) {
      if (e instanceof APIError && [400, 415, 422].includes(e.status))
        setCommand(null);
      setError(e instanceof Error ? e.message : "Unable to create incident");
    }
  });
  return (
    <>
      <Button onClick={() => setOpen(true)}>
        {command ? "Resume incident creation" : "Create incident"}
      </Button>
      <Dialog
        open={open}
        onOpenChange={(v) => {
          if (!isSubmitting) setOpen(v);
        }}
      >
        <DialogContent className="incident-editor">
          <DialogHeader>
            <DialogTitle>Create incident</DialogTitle>
            <DialogDescription>
              {command?.input.environment ?? environment} · starts in Detected ·
              recorded under {session.kind === "authenticated" ? session.user.username : "local-operator"}
            </DialogDescription>
          </DialogHeader>
          <form className="incident-edit-form" onSubmit={submit}>
            <fieldset
              className="incident-edit-fields"
              disabled={isSubmitting || !!command}
            >
              <div>
                <Label htmlFor="create-title">Title</Label>
                <Input
                  id="create-title"
                  {...register("title")}
                  aria-invalid={!!errors.title}
                />
                {errors.title && <p role="alert">{errors.title.message}</p>}
              </div>
              <div>
                <Label htmlFor="create-service">Affected service</Label>
                <NativeSelect id="create-service" {...register("service")}>
                  <NativeSelectOption value="">
                    Select a service
                  </NativeSelectOption>
                  {[
                    "api-gateway",
                    "subscriber-service",
                    "package-service",
                    "payment-service",
                    "notification-service",
                    "incident-service",
                  ].map((s) => (
                    <NativeSelectOption key={s} value={s}>
                      {s}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
                {errors.service && <p role="alert">{errors.service.message}</p>}
              </div>
              <div>
                <Label htmlFor="create-severity">Severity</Label>
                <NativeSelect id="create-severity" {...register("severity")}>
                  {["SEV-1", "SEV-2", "SEV-3", "SEV-4"].map((s) => (
                    <NativeSelectOption key={s} value={s}>
                      {s}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
              </div>
              <div>
                <Label htmlFor="create-owner">Owner (optional)</Label>
                <Input id="create-owner" {...register("owner")} />
                {errors.owner && <p role="alert">{errors.owner.message}</p>}
              </div>
              <div>
                <Label htmlFor="create-impact">Impact</Label>
                <textarea id="create-impact" rows={4} {...register("impact")} />
                {errors.impact && <p role="alert">{errors.impact.message}</p>}
              </div>
            </fieldset>
            {error && (
              <div role="alert" className="incident-edit-error">
                <p>{error}</p>
                {command && (
                  <p>
                    The result is uncertain. Retry this exact request to recover
                    the incident without duplicating it. Keep this page open
                    until confirmed.
                  </p>
                )}
              </div>
            )}
            <div className="page-actions">
              <Button
                type="button"
                variant="outline"
                disabled={isSubmitting}
                onClick={() => setOpen(false)}
              >
                {command ? "Close and keep request" : "Cancel"}
              </Button>
              <Button type="submit" disabled={isSubmitting}>
                {isSubmitting
                  ? "Recording…"
                  : command
                    ? "Retry same request"
                    : "Record incident"}
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}
