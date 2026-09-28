import { DeploymentDetail } from "@/components/deployments";
export const metadata = { title: "Deployment record" };
export default async function Page({ params }: { params: Promise<{ id: string }> }) { const { id } = await params; return <DeploymentDetail id={id} />; }
