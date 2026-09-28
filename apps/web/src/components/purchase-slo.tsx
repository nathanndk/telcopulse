import type { Overview } from "@/lib/contracts";
import { Panel } from "./common";

const count = new Intl.NumberFormat("en-US", { maximumFractionDigits: 3 });

export function PurchaseSLO({ slo }: { slo: Overview["purchase_slo"] }) {
  const measured = slo.current_percent !== null;
  const exhausted = measured && slo.budget_remaining === 0 && slo.failed > 0;
  return (
    <Panel
      title="Synthetic purchase success objective"
      description="Rolling 30 days · completed development or staging purchases in the selected environment"
    >
      <dl className="slo-grid">
        <div>
          <dt>Current SLI</dt>
          <dd className={exhausted ? "error-text" : undefined}>
            {slo.current_percent === null ? "—" : `${slo.current_percent.toFixed(2)}%`}
          </dd>
          <small>{measured ? `${slo.success} of ${slo.total} succeeded` : "No completed purchases"}</small>
        </div>
        <div>
          <dt>Target SLO</dt>
          <dd>{slo.target_percent.toFixed(1)}%</dd>
          <small>Observed business outcomes</small>
        </div>
        <div>
          <dt>Error budget remaining</dt>
          <dd>{measured ? count.format(slo.budget_remaining) : "—"}</dd>
          <small>{measured ? `of ${count.format(slo.allowed_failures)} allowed failure equivalents` : "Awaiting traffic"}</small>
        </div>
        <div>
          <dt>Budget consumed</dt>
          <dd className={exhausted ? "error-text" : undefined}>
            {slo.budget_consumed_percent === null
              ? "—"
              : `${count.format(slo.budget_consumed_percent)}%`}
          </dd>
          <small>{measured ? `${slo.failed} failed outcomes` : "No measurement"}</small>
        </div>
      </dl>
      <p className="panel-note">
        All failed synthetic purchases count, including intentional declines and
        injected faults. This is a traffic-based business objective, not a
        customer-facing availability commitment. The 30-day window differs from
        the one-hour charts above.
      </p>
    </Panel>
  );
}
