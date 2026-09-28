import { connection } from "next/server";
import { traceViewerBase } from "@/lib/trace-links";
import { TransactionDetail } from "@/components/transaction-detail";
export const metadata = { title: "Transaction investigation" };
export default async function Page({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  await connection();
  const { id } = await params;
  return <TransactionDetail id={id} traceViewer={traceViewerBase(process.env.TRACE_VIEWER_URL)} />;
}
