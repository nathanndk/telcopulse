import { Smartphone, Wifi, ShieldCheck, CreditCard } from "lucide-react";
import { money } from "@/lib/api";
import type { Customer, Package, Environment } from "@/lib/contracts";
import { Badge } from "./ui/badge";
export function CustomerPreview({
  customer,
  pkg,
  paymentMethod,
  environment,
}: {
  customer?: Customer;
  pkg?: Package;
  paymentMethod?: string;
  environment: Environment;
}) {
  return (
    <div className="preview-layout">
      <div className="phone-preview">
        <div className="phone-status">
          9:41 <Wifi size={13} />
        </div>
        <div className="phone-brand">
          <ActivityMark />
          TelcoPulse
        </div>
        <div className="phone-welcome">
          <small>Hello, {customer?.name.split(" ")[0]}</small>
          <strong>Stay connected.</strong>
          <span>{customer?.msisdn_masked}</span>
        </div>
        <div className="phone-balance">
          <small>Available pulsa</small>
          <strong>{money(customer?.balance_idr ?? 0)}</strong>
        </div>
        <div className="phone-package">
          <Wifi size={24} />
          <strong>{pkg?.data_gb} GB</strong>
          <span>{pkg?.name}</span>
          <small>
            {pkg?.days} days · {money(pkg?.price_idr ?? 0)}
          </small>
          <Badge variant="secondary">Selected package</Badge>
        </div>
        <div className="phone-bottom">
          <Smartphone size={16} /> Your connection, simplified
        </div>
      </div>
      <div className="customer-summary">
        <h3>Customer summary</h3>
        <dl>
          <dt>Subscriber</dt>
          <dd>{customer?.name}</dd>
          <dt>MSISDN</dt>
          <dd className="mono">{customer?.msisdn_masked}</dd>
          <dt>Payment</dt>
          <dd>
            <CreditCard size={14} />
            {paymentMethod}
          </dd>
          <dt>Environment</dt>
          <dd>{environment}</dd>
        </dl>
        <div className="ready-state">
          <ShieldCheck size={18} />
          <div>
            Ready to simulate
            <small>
              Creates a persisted transaction with correlated evidence.
            </small>
          </div>
        </div>
      </div>
    </div>
  );
}
function ActivityMark() {
  return (
    <span aria-hidden="true" className="activity-mark">
      ı|ı
    </span>
  );
}
