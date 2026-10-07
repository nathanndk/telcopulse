import type { Overview } from "@/lib/contracts";
import { Panel } from "./common";

const count = new Intl.NumberFormat("en-US", { maximumFractionDigits: 3 });

export function PurchaseSLO({ slo }: { slo: Overview["purchase_slo"] }) {
  const measured = slo.current_percent !== null;
  const exhausted = measured && slo.budget_remaining === 0 && slo.technical_failed > 0;
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
          <small>{measured ? `${slo.total - slo.technical_failed} of ${slo.total} without technical failure` : "No completed purchases"}</small>
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
          <small>{measured ? `${slo.technical_failed} technical failures` : "No measurement"}</small>
        </div>
        <div>
          <dt>Business declines</dt>
          <dd>{measured ? count.format(slo.business_declined) : "—"}</dd>
          <small>{measured ? "Expected refusals, not counted against budget" : "No measurement"}</small>
        </div>
      </dl>
      <p className="panel-note">
        Technical failures, including injected faults, burn the error budget.
        Business declines such as insufficient balance are reported separately
        and count as the platform working correctly. This is a traffic-based business objective, not a
        customer-facing availability commitment. The 30-day window differs from
        the one-hour charts above.
      </p>
    </Panel>
  );
}

export function PurchaseLatencySLO({ slo }: { slo: Overview["purchase_latency_slo"] }) {
  const measured = slo.current_percent !== null;
  const exhausted = measured && slo.budget_remaining === 0 && slo.slow > 0;
  const seconds = count.format(slo.threshold_ms / 1000);
  return (
    <Panel
      title="Synthetic purchase latency objective"
      description={`Rolling 30 days · completed purchases of any outcome finishing within ${seconds} s`}
    >
      <dl className="slo-grid">
        <div>
          <dt>Current SLI</dt>
          <dd className={exhausted ? "error-text" : undefined}>
            {slo.current_percent === null ? "—" : `${slo.current_percent.toFixed(2)}%`}
          </dd>
          <small>{measured ? `${slo.total - slo.slow} of ${slo.total} within ${seconds} s` : "No completed purchases"}</small>
        </div>
        <div>
          <dt>Target SLO</dt>
          <dd>{slo.target_percent.toFixed(1)}%</dd>
          <small>Creation to completion, including recovery</small>
        </div>
        <div>
          <dt>Error budget remaining</dt>
          <dd>{measured ? count.format(slo.budget_remaining) : "—"}</dd>
          <small>{measured ? `of ${count.format(slo.allowed_slow)} allowed slow purchases` : "Awaiting traffic"}</small>
        </div>
        <div>
          <dt>Budget consumed</dt>
          <dd className={exhausted ? "error-text" : undefined}>
            {slo.budget_consumed_percent === null ? "—" : `${count.format(slo.budget_consumed_percent)}%`}
          </dd>
          <small>{measured ? `${slo.slow} slower than ${seconds} s` : "No measurement"}</small>
        </div>
      </dl>
      <p className="panel-note">
        Duration is recorded by the gateway when a purchase reaches its final
        outcome, so a purchase held in Processing during a dependency outage counts
        as slow once it recovers. Purchases still processing are not yet counted.
      </p>
    </Panel>
  );
}
