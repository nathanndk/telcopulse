import type { ReactNode } from "react";
import { AlertCircle, CheckCircle2, CircleHelp, Activity } from "lucide-react";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Empty,
  EmptyHeader,
  EmptyTitle,
  EmptyDescription,
  EmptyMedia,
} from "@/components/ui/empty";
export function PageHeader({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children?: ReactNode;
}) {
  return (
    <div className="page-header">
      <div>
        <div className="breadcrumb">
          Operations <span>/</span> {title}
        </div>
        <h1>{title}</h1>
        <p>{description}</p>
      </div>
      <div className="page-actions">{children}</div>
    </div>
  );
}
export function Panel({
  title,
  description,
  action,
  children,
  className = "",
}: {
  title: string;
  description?: string;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={`panel ${className}`}>
      <header className="panel-header">
        <div>
          <h2>{title}</h2>
          {description && <p>{description}</p>}
        </div>
        {action}
      </header>
      {children}
    </section>
  );
}
export function StatusBadge({ status }: { status: string }) {
  const good = ["SUCCESS", "Healthy"].includes(status);
  const bad = ["FAILED", "Critical"].includes(status);
  return (
    <Badge
      variant="outline"
      className={
        good
          ? "status-success"
          : bad
            ? "status-error"
            : status === "Degraded"
              ? "status-warning"
              : "status-neutral"
      }
    >
      {good ? <CheckCircle2 /> : bad ? <AlertCircle /> : <CircleHelp />}
      {status === "SUCCESS"
        ? "Success"
        : status === "FAILED"
          ? "Failed"
          : status === "PROCESSING"
            ? "Processing"
            : status}
    </Badge>
  );
}
export function ErrorState({
  error,
  retry,
}: {
  error: Error;
  retry: () => void;
}) {
  return (
    <Alert variant="destructive">
      <AlertCircle />
      <AlertTitle>Unable to load this view</AlertTitle>
      <AlertDescription>
        <p>{error.message}</p>
        <Button variant="outline" size="sm" onClick={retry}>
          Try again
        </Button>
      </AlertDescription>
    </Alert>
  );
}
export function LoadingState() {
  return (
    <div aria-label="Loading data" role="status" className="loading-grid">
      <Skeleton className="h-28" />
      <Skeleton className="h-28" />
      <Skeleton className="h-64 col-span-2" />
      <span className="sr-only">Loading data</span>
    </div>
  );
}
export function EmptyState({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children?: ReactNode;
}) {
  return (
    <Empty>
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <Activity />
        </EmptyMedia>
        <EmptyTitle>{title}</EmptyTitle>
        <EmptyDescription>{description}</EmptyDescription>
      </EmptyHeader>
      {children}
    </Empty>
  );
}
