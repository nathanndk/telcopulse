"use client";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { APIError } from "@/lib/api";
import { usePermission } from "./providers";
import {
  simulations,
  simulationInput,
  type SimulationCommand,
  type SimulationInput,
  type SimulationRun,
} from "@/lib/simulations";
import type { Environment } from "@/lib/contracts";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { NativeSelect, NativeSelectOption } from "./ui/native-select";
import { Label } from "./ui/label";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "./ui/dialog";

export function StartSimulation({
  environment,
  disabled,
}: {
  environment: Environment;
  disabled: boolean;
}) {
  const canInject = usePermission("injectFailure");
  const [open, setOpen] = useState(false);
  const [error, setError] = useState("");
  const [command, setCommand] = useState<{
    input: SimulationCommand;
    key: string;
  } | null>(null);
  const client = useQueryClient();
  const {
    register,
    control,
    handleSubmit,
    reset,
    formState: { errors, isSubmitting },
  } = useForm<SimulationInput>({
    resolver: zodResolver(simulationInput),
    defaultValues: {
      scenario: "payment-decline",
      delay_ms: 4200,
      percentage: 10,
      duration_seconds: 120,
      reason: "",
    },
  });
  const scenario = useWatch({ control, name: "scenario" });
  const submit = handleSubmit(async (values) => {
    const attempt = command ?? {
      input: {
        ...values,
        delay_ms: values.scenario === "payment-decline" || values.scenario === "bad-deployment" ? 0 : values.delay_ms,
        environment,
      },
      key: crypto.randomUUID(),
    };
    setCommand(attempt);
    setError("");
    try {
      const result = await simulations.start(attempt.input, attempt.key);
      await client.invalidateQueries({ queryKey: ["simulations"] });
      setCommand(null);
      reset();
      setOpen(false);
      toast.success(
        result.active
          ? `Failure simulation active in ${result.environment}`
          : "Original simulation confirmed; it is no longer active",
      );
    } catch (e) {
      if (e instanceof APIError && [400, 409, 415, 422].includes(e.status))
        setCommand(null);
      setError(e instanceof Error ? e.message : "Unable to start simulation");
      await client.invalidateQueries({ queryKey: ["simulations"] });
    }
  });
  if (!canInject) return null;
  return (
    <>
      <Button
        variant="outline"
        disabled={disabled && !command}
        onClick={() => setOpen(true)}
      >
        {command ? "Resume simulation request" : "Inject failure"}
      </Button>
      <Dialog
        open={open}
        onOpenChange={(value) => {
          if (!isSubmitting) setOpen(value);
        }}
      >
        <DialogContent className="incident-editor">
          <DialogHeader>
            <DialogTitle>Review failure simulation</DialogTitle>
            <DialogDescription>
              {command?.input.environment ?? environment} ·{" "}
              {command?.input.scenario ?? scenario} · local-operator
            </DialogDescription>
          </DialogHeader>
          <form onSubmit={submit} className="incident-edit-form">
            <p>
              {scenario === "bad-deployment"
                ? "A synthetic payment-service release marker is reported before selected new reservations can fail with BAD_DEPLOYMENT_PAYMENT_FAILURE. This changes simulated behavior, not a container image. Stop or expiry queues a rollback marker and restores new purchases."
                : scenario === "database-timeout"
                ? "Selected payment queries reach a real PostgreSQL statement timeout. Purchases fail with DB_TIMEOUT without debiting the payment balance; retries preserve the recorded outcome."
                : scenario === "kafka-consumer-lag"
                  ? "Selected notifications wait before delivery and Kafka offset commit. Purchases can still succeed. The shared consumer can also delay other environments behind selected events."
                  : scenario === "database-latency"
                    ? "Selected new payment reservations execute a real bounded PostgreSQL delay. Purchases can still succeed; latency and connection use increase. Other environments are unaffected."
                    : "New purchases in this environment may fail with SIMULATED_PAYMENT_DECLINED. No payment balance is debited for an injected decline. Other environments are unaffected."}
            </p>
            <fieldset
              className="incident-edit-fields"
              disabled={isSubmitting || !!command}
            >
              <div>
                <Label htmlFor="failure-scenario">Failure scenario</Label>
                <NativeSelect id="failure-scenario" {...register("scenario")}>
                  <NativeSelectOption value="payment-decline">
                    Payment decline
                  </NativeSelectOption>
                  <NativeSelectOption value="database-latency">
                    Database latency
                  </NativeSelectOption>
                  <NativeSelectOption value="database-timeout">
                    Database timeout
                  </NativeSelectOption>
                  <NativeSelectOption value="kafka-consumer-lag">
                    Kafka consumer lag
                  </NativeSelectOption>
                  <NativeSelectOption value="bad-deployment">
                    Bad deployment (synthetic)
                  </NativeSelectOption>
                </NativeSelect>
              </div>
              {scenario !== "payment-decline" && scenario !== "bad-deployment" && (
                <div>
                  <Label htmlFor="failure-delay">
                    {scenario === "kafka-consumer-lag"
                      ? "Notification delay (ms)"
                      : scenario === "database-timeout"
                        ? "Statement timeout (ms)"
                        : "Database delay (ms)"}
                  </Label>
                  <Input
                    id="failure-delay"
                    type="number"
                    min={100}
                    max={4500}
                    {...register("delay_ms", { valueAsNumber: true })}
                  />
                  {errors.delay_ms && <p role="alert">Use 100–4500 ms.</p>}
                </div>
              )}
              <div>
                <Label htmlFor="failure-percentage">Selection percentage</Label>
                <Input
                  id="failure-percentage"
                  type="number"
                  min={1}
                  max={100}
                  {...register("percentage", { valueAsNumber: true })}
                />
                {errors.percentage && (
                  <p role="alert">Choose an integer from 1 to 100.</p>
                )}
              </div>
              <div>
                <Label htmlFor="failure-duration">Duration in seconds</Label>
                <Input
                  id="failure-duration"
                  type="number"
                  min={30}
                  max={900}
                  {...register("duration_seconds", { valueAsNumber: true })}
                />
                {errors.duration_seconds && (
                  <p role="alert">Choose 30 to 900 seconds.</p>
                )}
              </div>
              <div>
                <Label htmlFor="failure-reason">Simulation reason</Label>
                <Input
                  id="failure-reason"
                  maxLength={1000}
                  {...register("reason")}
                />
                {errors.reason && <p role="alert">{errors.reason.message}</p>}
              </div>
            </fieldset>
            <p className="muted">
              {scenario === "bad-deployment"
                ? "Fault selection begins only after the deployment marker is confirmed. Stop or expiry queues an audited rollback event; new purchases then recover. Existing transaction decisions remain stable on retry."
                : scenario === "database-timeout"
                ? "Stop or expiry prevents new selections. Already recorded timeout outcomes remain unchanged on retry."
                : scenario === "kafka-consumer-lag"
                  ? "Stop or expiry prevents new notification waits. An in-flight wait can take up to 4.5 seconds to finish; queued events then drain at normal capacity."
                  : "The run expires automatically. Stopping affects new decisions; retries retain their original result."}{" "}
              You can stop this run from the history below.
            </p>
            {command && (
              <p role="status">
                Confirmation pending for {command.input.environment}. Keep this
                page open and retry this exact request; its key does not survive
                navigation or reload.
              </p>
            )}
            {error && (
              <p role="alert" className="error-text">
                {error}
              </p>
            )}
            <div className="page-actions">
              <Button
                type="button"
                variant="outline"
                disabled={isSubmitting}
                onClick={() => setOpen(false)}
              >
                Close
              </Button>
              <Button type="submit" disabled={isSubmitting}>
                {isSubmitting
                  ? "Confirming…"
                  : command
                    ? "Retry same start request"
                    : "Start failure simulation"}
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}

export function StopSimulation({ run }: { run: SimulationRun }) {
  const canInject = usePermission("injectFailure");
  const [open, setOpen] = useState(false);
  const [reason, setReason] = useState("");
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);
  const client = useQueryClient();
  if (!canInject) return null;
  return (
    <>
      <Button variant="outline" onClick={() => setOpen(true)}>
        Stop simulation
      </Button>
      <Dialog
        open={open}
        onOpenChange={(value) => {
          if (!pending) setOpen(value);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Stop failure simulation</DialogTitle>
            <DialogDescription>
              {run.id} · {run.environment}
            </DialogDescription>
          </DialogHeader>
          <form
            className="incident-edit-form"
            onSubmit={async (event) => {
              event.preventDefault();
              if (!reason.trim()) {
                setError("A stop reason is required");
                return;
              }
              setPending(true);
              setError("");
              try {
                await simulations.stop(run.id, reason.trim());
                setOpen(false);
                await client.invalidateQueries({ queryKey: ["simulations"] });
                toast.success("Simulation stopped; new decisions can recover");
              } catch (e) {
                setError(
                  e instanceof Error
                    ? e.message
                    : "Unable to confirm stop; retry this run",
                );
              } finally {
                setPending(false);
              }
            }}
          >
            <p>
              This stops new injected decisions. Previously selected failures
              remain stable on retry. {run.scenario === "bad-deployment" ? "A rollback event is queued for reporting. " : ""}
              Alerts may take time to clear; incidents remain open until an operator resolves them.
            </p>
            <Label htmlFor={`stop-reason-${run.id}`}>Stop reason</Label>
            <Input
              id={`stop-reason-${run.id}`}
              required
              maxLength={1000}
              value={reason}
              disabled={pending}
              onChange={(event) => setReason(event.target.value)}
            />
            {error && (
              <p role="alert" className="error-text">
                {error}
              </p>
            )}
            <div className="page-actions">
              <Button
                type="button"
                variant="outline"
                disabled={pending}
                onClick={() => setOpen(false)}
              >
                Cancel
              </Button>
              <Button type="submit" disabled={pending}>
                {pending ? "Stopping…" : "Confirm stop"}
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}
