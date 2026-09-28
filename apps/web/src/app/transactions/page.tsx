import { PageHeader, Panel } from "@/components/common";
import { TransactionTable } from "@/components/transaction-table";
export const metadata = { title: "Transactions" };
export default async function Page({
  searchParams,
}: {
  searchParams: Promise<{ search?: string }>;
}) {
  const { search } = await searchParams;
  return (
    <>
      <PageHeader
        title="Transactions"
        description="Follow customer purchases and investigate their business outcomes."
      />
      <Panel
        title="Transaction explorer"
        description="Server-filtered results · newest transactions first"
      >
        <TransactionTable key={search} initialSearch={search} />
      </Panel>
    </>
  );
}
