import { IncidentDetail } from "@/components/incidents";
import { traceViewerBase } from "@/lib/trace-links";
export const metadata = { title: "Incident investigation" };
export default async function Page({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  return <IncidentDetail id={id} traceViewer={traceViewerBase(process.env.TRACE_VIEWER_URL)} />;
}
