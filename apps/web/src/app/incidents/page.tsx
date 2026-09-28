import { PageHeader, Panel } from "@/components/common";
import { IncidentList } from "@/components/incidents";
export const metadata = { title: "Incidents" };
export default function Page() {
  return (
    <>
      <PageHeader
        title="Incidents"
        description="Track service impact, investigation and recovery across the incident lifecycle."
      />
      <Panel
        title="Incident register"
        description="Search, sort and review persisted incidents"
      >
        <IncidentList />
      </Panel>
    </>
  );
}
