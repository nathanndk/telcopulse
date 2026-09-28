"use client";
import { SimulationRuns } from "./simulation-runs";
import { useRef, useState } from "react";
import Link from "next/link";
import { useForm, useWatch } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowRight,
  ArrowUpRight,
  Check,
  FlaskConical,
  LoaderCircle,
  Play,
  Wifi,
  ShieldCheck,
} from "lucide-react";
import { toast } from "sonner";
import { api, money, duration } from "@/lib/api";
import {
  purchaseSchema,
  type Purchase,
  type Transaction,
} from "@/lib/contracts";
import {
  PageHeader,
  Panel,
  ErrorState,
  LoadingState,
  StatusBadge,
} from "./common";
import { useEnvironment, usePermission } from "./providers";
import { Button } from "./ui/button";
import { Badge } from "./ui/badge";
import {
  Field,
  FieldGroup,
  FieldLabel,
  FieldDescription,
  FieldError,
} from "./ui/field";
import { NativeSelect, NativeSelectOption } from "./ui/native-select";
import { Alert, AlertDescription, AlertTitle } from "./ui/alert";
import { CustomerPreview } from "./customer-preview";
import { TransactionTable } from "./transaction-table";
import { ReplayFailedTransaction } from "./replay-failed-transaction";
export function Simulator() {
  const { environment } = useEnvironment();
  const canPurchase = usePermission("runPurchase");
  const client = useQueryClient();
  const [result, setResult] = useState<Transaction | null>(null);
  const requestRef = useRef<{ body: string; key: string } | null>(null);
  const customers = useQuery({
    queryKey: ["customers"],
    queryFn: api.customers,
  });
  const packages = useQuery({ queryKey: ["packages"], queryFn: api.packages });
  const form = useForm<Purchase>({
    resolver: zodResolver(purchaseSchema),
    defaultValues: {
      customer_id: "cus-001",
      package_id: "pkg-10",
      payment_method: "E-Wallet",
      environment,
    },
  });
  const values = useWatch({ control: form.control });
  const customer = customers.data?.find((c) => c.id === values.customer_id);
  const pkg = packages.data?.find((p) => p.id === values.package_id);
  const mutation = useMutation({
    mutationFn: async (data: Purchase) => {
      const body = JSON.stringify(data);
      if (requestRef.current?.body !== body)
        requestRef.current = { body, key: crypto.randomUUID() };
      return api.purchase(data, requestRef.current.key);
    },
    onSuccess: async (data) => {
      setResult(data);
      requestRef.current = null;
      await Promise.all([
        client.invalidateQueries({ queryKey: ["transactions"] }),
        client.invalidateQueries({ queryKey: ["overview"] }),
        client.invalidateQueries({ queryKey: ["customers"] }),
      ]);
      if (data.status === "SUCCESS")
        toast.success("Package activated", {
          description: `${data.package_name} · ${data.msisdn_masked}`,
        });
      else if (data.status === "PROCESSING")
        toast.info("Purchase accepted", {
          description:
            "Recovery is in progress. Open the transaction to follow its status.",
        });
      else
        toast.error("Business transaction failed", {
          description: data.error_code.replaceAll("_", " ").toLowerCase(),
        });
    },
    onError: (error) =>
      toast.error("Transaction could not be confirmed", {
        description: error.message,
      }),
  });
  return (
    <>
      <PageHeader
        title="Customer Simulator"
        description="Run a customer journey. Follow the evidence from purchase to activation."
      >
        <Badge variant="outline">
          <FlaskConical /> Synthetic environment
        </Badge>
      </PageHeader>
      <SimulationRuns environment={environment} />
      <ol className="journey-steps">
        {[
          "Select customer",
          "Choose package",
          "Configure payment",
          "Run transaction",
          "View result",
        ].map((text, index) => (
          <li
            key={text}
            className={result ? "complete" : index === 0 ? "current" : ""}
          >
            <span>{result ? <Check size={15} /> : index + 1}</span>
            <div>
              {text}
              <small>
                {
                  [
                    "Subscriber & MSISDN",
                    "Data & validity",
                    "Synthetic provider",
                    "Persist purchase",
                    "Investigate evidence",
                  ][index]
                }
              </small>
            </div>
            {index < 4 && <ArrowRight size={16} />}
          </li>
        ))}
      </ol>
      {customers.isPending || packages.isPending ? (
        <LoadingState />
      ) : customers.isError || packages.isError ? (
        <ErrorState
          error={
            customers.error ??
            packages.error ??
            new Error("Catalog unavailable")
          }
          retry={() => {
            customers.refetch();
            packages.refetch();
          }}
        />
      ) : (
        <div className="simulator-grid">
          <Panel
            title="Transaction configuration"
            description="Select a synthetic subscriber and configure their purchase."
          >
            <form
              className="simulation-form"
              onSubmit={form.handleSubmit((data) => {
                if (canPurchase) mutation.mutate({ ...data, environment });
              })}
            >
              <FieldGroup>
                <div className="form-grid">
                  <Field data-invalid={!!form.formState.errors.customer_id}>
                    <FieldLabel htmlFor="customer">
                      Customer / MSISDN
                    </FieldLabel>
                    <NativeSelect
                      id="customer"
                      {...form.register("customer_id")}
                      aria-invalid={!!form.formState.errors.customer_id}
                    >
                      {customers.data?.map((c) => (
                        <NativeSelectOption key={c.id} value={c.id}>
                          {c.name} · {c.msisdn_masked}
                        </NativeSelectOption>
                      ))}
                    </NativeSelect>
                    <FieldDescription>
                      Fictional subscribers. Phone numbers are masked.
                    </FieldDescription>
                    <FieldError errors={[form.formState.errors.customer_id]} />
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="simulation-env">
                      Environment
                    </FieldLabel>
                    <NativeSelect
                      id="simulation-env"
                      value={environment}
                      disabled
                    >
                      <NativeSelectOption value={environment}>
                        {environment === "development"
                          ? "Development"
                          : "Staging"}
                      </NativeSelectOption>
                    </NativeSelect>
                    <FieldDescription>
                      Change the workspace environment in the header.
                    </FieldDescription>
                  </Field>
                </div>
                <Field data-invalid={!!form.formState.errors.package_id}>
                  <FieldLabel htmlFor="package">Internet package</FieldLabel>
                  <NativeSelect
                    id="package"
                    {...form.register("package_id")}
                    aria-invalid={!!form.formState.errors.package_id}
                  >
                    {packages.data?.map((p) => (
                      <NativeSelectOption key={p.id} value={p.id}>
                        {p.name} · {p.data_gb} GB / {p.days} days ·{" "}
                        {money(p.price_idr)}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                  <FieldError errors={[form.formState.errors.package_id]} />
                </Field>
                <Field>
                  <FieldLabel htmlFor="payment">Payment method</FieldLabel>
                  <NativeSelect
                    id="payment"
                    {...form.register("payment_method")}
                  >
                    {[
                      "Pulsa",
                      "E-Wallet",
                      "Credit Card",
                      "Virtual Account",
                    ].map((method) => (
                      <NativeSelectOption value={method} key={method}>
                        {method}
                      </NativeSelectOption>
                    ))}
                  </NativeSelect>
                  <FieldDescription>
                    {values.payment_method === "Pulsa"
                      ? `Deducts from the synthetic balance: ${money(customer?.balance_idr ?? 0)}.`
                      : "The local payment adapter approves this method. No real charges."}
                  </FieldDescription>
                </Field>
              </FieldGroup>
              <div className="purchase-summary">
                <span>
                  <Wifi size={20} />
                  <span>
                    {pkg?.data_gb} GB <small>{pkg?.days} days validity</small>
                  </span>
                </span>
                <strong>{money(pkg?.price_idr ?? 0)}</strong>
              </div>
              {mutation.isError && (
                <Alert variant="destructive">
                  <AlertTitle>Result not confirmed</AlertTitle>
                  <AlertDescription>
                    {mutation.error.message} Retry with the same configuration;
                    the request key prevents a duplicate purchase.
                  </AlertDescription>
                </Alert>
              )}
              <div className="form-actions">
                <Button type="submit" disabled={mutation.isPending || !canPurchase} size="lg">
                  {mutation.isPending ? (
                    <LoaderCircle className="spin" data-icon="inline-start" />
                  ) : (
                    <Play data-icon="inline-start" />
                  )}
                  {!canPurchase ? "Viewer role cannot run transactions" : mutation.isPending
                    ? "Processing purchase…"
                    : "Start synthetic transaction"}
                </Button>
                <span>
                  <ShieldCheck size={14} /> Audited & idempotent
                </span>
              </div>
            </form>
          </Panel>
          <Panel
            title="Customer experience preview"
            description="A live summary of the configuration you are about to run."
          >
            <CustomerPreview
              customer={customer}
              pkg={pkg}
              paymentMethod={values.payment_method}
              environment={environment}
            />
          </Panel>
        </div>
      )}
      {result && (
        <Panel
          title="Simulation result"
          action={
            <Button variant="outline" size="sm" asChild>
              <Link href={`/transactions/${result.id}`}>
                Investigate transaction
                <ArrowUpRight />
              </Link>
            </Button>
          }
        >
          <div className="result-summary">
            <StatusBadge status={result.status} />
            <strong>
              {result.status === "SUCCESS"
                ? "Package activation completed"
                : result.status === "PROCESSING"
                  ? "Purchase processing"
                  : "Purchase failed"}
            </strong>
            <span className="mono">{duration(result.duration_ms)}</span>
            <span className="mono muted">{result.id}</span>
          </div>
          <ReplayFailedTransaction transaction={result} />
          {result.error_code && (
            <p className="panel-note error-text">
              {result.error_code}:{" "}
              {result.error_code === "INSUFFICIENT_BALANCE"
                ? "Select E-Wallet or a subscriber with enough balance, then run a new transaction."
                : "Inspect the transaction evidence."}
            </p>
          )}
        </Panel>
      )}
      <Panel
        title="Recent simulated transactions"
        description="Every run is stored with a unique transaction and trace ID."
        action={
          <Button variant="link" size="sm" asChild>
            <Link href="/transactions">
              View all
              <ArrowUpRight />
            </Link>
          </Button>
        }
      >
        <TransactionTable compact key={environment} />
      </Panel>
    </>
  );
}
