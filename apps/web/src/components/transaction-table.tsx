"use client";
import { useMemo, useState } from "react";
import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  useReactTable,
} from "@tanstack/react-table";
import { ArrowUpRight, ChevronLeft, ChevronRight, Search } from "lucide-react";
import { api, duration, timestamp } from "@/lib/api";
import type { Transaction } from "@/lib/contracts";
import { useEnvironment } from "./providers";
import { EmptyState, ErrorState, LoadingState, StatusBadge } from "./common";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { NativeSelect, NativeSelectOption } from "./ui/native-select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "./ui/table";
const helper = createColumnHelper<Transaction>();
export function TransactionTable({
  compact = false,
  initialSearch = "",
}: {
  compact?: boolean;
  initialSearch?: string;
}) {
  const { environment } = useEnvironment();
  const [page, setPage] = useState(1);
  const [status, setStatus] = useState("");
  const [search, setSearch] = useState(initialSearch);
  const query = useQuery({
    queryKey: ["transactions", environment, page, status, search, compact],
    queryFn: () =>
      api.transactions(environment, page, status, search, compact ? 5 : 20),
  });
  const columns = useMemo(
    () => [
      helper.accessor("id", {
        header: "Transaction",
        cell: (info) => (
          <Link
            className="table-link mono"
            href={`/transactions/${info.getValue()}`}
          >
            {info.getValue().slice(0, 16)}…
          </Link>
        ),
      }),
      helper.accessor("created_at", {
        header: "Timestamp (WIB)",
        cell: (info) => (
          <span className="muted nowrap">{timestamp(info.getValue())}</span>
        ),
      }),
      helper.accessor("msisdn_masked", {
        header: "Customer",
        cell: (info) => (
          <div className="table-stack">
            <span>{info.row.original.customer_name}</span>
            <small className="mono">{info.getValue()}</small>
          </div>
        ),
      }),
      helper.accessor("package_name", { header: "Package" }),
      helper.accessor("payment_method", { header: "Payment" }),
      helper.accessor("status", {
        header: "Result",
        cell: (info) => <StatusBadge status={info.getValue()} />,
      }),
      helper.accessor("duration_ms", {
        header: "Duration",
        cell: (info) => (
          <span className="mono">{duration(info.getValue())}</span>
        ),
      }),
      helper.display({
        id: "action",
        header: "",
        cell: (info) => (
          <Button size="sm" variant="ghost" asChild>
            <Link
              href={`/transactions/${info.row.original.id}`}
              aria-label={`Investigate ${info.row.original.id}`}
            >
              Investigate
              <ArrowUpRight data-icon="inline-end" />
            </Link>
          </Button>
        ),
      }),
    ],
    [],
  );
  // TanStack Table intentionally owns its mutable table instance.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data: query.data?.items ?? [],
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    pageCount: Math.ceil((query.data?.total ?? 0) / 20),
  });
  return (
    <div>
      {!compact && (
        <div className="table-toolbar">
          <div className="search-field">
            <Search size={15} />
            <Input
              aria-label="Search transactions"
              placeholder="Search ID, trace or customer..."
              maxLength={100}
              value={search}
              onChange={(e) => {
                setSearch(e.target.value);
                setPage(1);
              }}
            />
          </div>
          <NativeSelect
            aria-label="Transaction status"
            value={status}
            onChange={(e) => {
              setStatus(e.target.value);
              setPage(1);
            }}
          >
            <NativeSelectOption value="">All results</NativeSelectOption>
            <NativeSelectOption value="SUCCESS">Success</NativeSelectOption>
            <NativeSelectOption value="FAILED">Failed</NativeSelectOption>
            <NativeSelectOption value="PROCESSING">
              Processing
            </NativeSelectOption>
          </NativeSelect>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              setStatus("");
              setSearch("");
              setPage(1);
            }}
          >
            Clear filters
          </Button>
          <span className="muted toolbar-end">Newest first</span>
        </div>
      )}
      {query.isPending ? (
        <LoadingState />
      ) : query.isError ? (
        <div className="panel-padding">
          <ErrorState error={query.error} retry={() => query.refetch()} />
        </div>
      ) : !query.data.items.length ? (
        <EmptyState
          title={
            search || status
              ? "No matching transactions"
              : "Your transaction stream starts here"
          }
          description={
            search || status
              ? "Adjust your search or filters to find a transaction."
              : "Run a synthetic purchase to capture its payment result, duration and correlated evidence."
          }
        >
          {!search && !status && (
            <Button asChild variant="outline">
              <Link href="/simulator">
                Open customer simulator
                <ArrowUpRight data-icon="inline-end" />
              </Link>
            </Button>
          )}
        </EmptyState>
      ) : (
        <>
          <Table>
            <TableHeader>
              {table.getHeaderGroups().map((group) => (
                <TableRow key={group.id}>
                  {group.headers.map((header) => (
                    <TableHead key={header.id}>
                      {flexRender(
                        header.column.columnDef.header,
                        header.getContext(),
                      )}
                    </TableHead>
                  ))}
                </TableRow>
              ))}
            </TableHeader>
            <TableBody>
              {table.getRowModel().rows.map((row) => (
                <TableRow key={row.id}>
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id}>
                      {flexRender(
                        cell.column.columnDef.cell,
                        cell.getContext(),
                      )}
                    </TableCell>
                  ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {!compact && (
            <div className="pagination">
              <span>
                {query.data.total} transactions · Page {page} of{" "}
                {Math.max(1, Math.ceil(query.data.total / 20))}
              </span>
              <div>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={page === 1}
                  onClick={() => setPage((p) => p - 1)}
                >
                  <ChevronLeft />
                  Previous
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={page * 20 >= query.data.total}
                  onClick={() => setPage((p) => p + 1)}
                >
                  Next
                  <ChevronRight />
                </Button>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
}
