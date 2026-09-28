import { SimulationDetail } from "@/components/simulation-detail";
export const metadata = { title: "Simulation investigation" };
export default async function Page({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  return <SimulationDetail key={id} id={id} />;
}
