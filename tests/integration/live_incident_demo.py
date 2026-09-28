"""Exercise one real synthetic incident through reviewed recovery and postmortem.

This intentionally takes more than ten minutes: recovery uses two actual five-minute
completion windows. It creates durable synthetic records and always stops its run.
"""

import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from datetime import datetime, timezone
from pathlib import Path


BASE = os.environ.get("WEB_URL", "http://localhost:3001").rstrip("/")
PROMETHEUS = os.environ.get("PROMETHEUS_URL", "http://localhost:9090").rstrip("/")
ENVIRONMENT = "development"
STARTED = datetime.now(timezone.utc)
EVIDENCE = {"started_at": STARTED.isoformat(), "environment": ENVIRONMENT}
RUN_ID = None
RUN_STOPPED = False


def api(path, payload=None, *, method=None, key=None):
    headers = {"Content-Type": "application/json"}
    if key:
        headers["Idempotency-Key"] = key
    request = urllib.request.Request(
        BASE + "/api/v1" + path,
        data=None if payload is None else json.dumps(payload).encode(),
        headers=headers,
        method=method or ("POST" if payload is not None else "GET"),
    )
    try:
        with urllib.request.urlopen(request, timeout=20) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as error:
        body = error.read(1000).decode(errors="replace")
        raise RuntimeError(f"{path} returned HTTP {error.code}: {body}") from error


def prometheus_alerts():
    with urllib.request.urlopen(PROMETHEUS + "/api/v1/alerts", timeout=10) as response:
        body = json.load(response)
    if body.get("status") != "success":
        raise RuntimeError("Prometheus alert response was unsuccessful")
    return [a for a in body["data"]["alerts"] if a.get("labels", {}).get("alertname") == "BusinessSuccessRateLow"
            and a.get("labels", {}).get("environment") == ENVIRONMENT]


def iso(value):
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def wait_for(label, callback, seconds=180):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        result = callback()
        if result:
            return result
        time.sleep(5)
    raise RuntimeError(f"timed out waiting for {label}")


def purchase(expected):
    command = {"customer_id": "cus-001", "package_id": "pkg-3", "payment_method": "E-Wallet", "environment": ENVIRONMENT}
    status, result = api("/transactions", command, key=str(uuid.uuid4()))
    if status not in (200, 201, 202):
        raise RuntimeError(f"purchase returned unexpected HTTP {status}")
    if result["status"] == "PROCESSING":
        transaction_id = result["id"]
        result = wait_for("terminal purchase " + transaction_id,
                          lambda: (lambda r: r if r["status"] != "PROCESSING" else None)(api("/transactions/" + transaction_id)[1]), 45)
    if result["status"] != expected:
        raise RuntimeError(f"expected {expected} purchase, got {result['status']} ({result['id']})")
    return result


def stop_run():
    global RUN_STOPPED
    if RUN_ID and not RUN_STOPPED:
        _, stopped = api("/simulations/" + RUN_ID + "/stop", {"reason": "Live incident exercise mitigation: restore healthy synthetic purchases"})
        if stopped.get("active"):
            raise RuntimeError("simulation still active after stop command")
        RUN_STOPPED = True
        print("Stopped controlled simulation", RUN_ID, flush=True)


def update_incident(incident, state, fields, note, validation=None):
    command = {**fields, "state": state, "expected_version": incident["version"], "note": note}
    if validation:
        command["recovery_validation"] = validation
    _, updated = api("/incidents/" + incident["id"], command, method="PUT")
    if updated["state"] != state or updated["version"] != incident["version"] + 1:
        raise RuntimeError("incident transition did not advance one revision")
    print("Incident", incident["id"], "→", state, "revision", updated["version"], flush=True)
    return updated


def optional_evidence(path):
    try:
        _, body = api(path)
        return {"http_status": 200, "status": body.get("status"), "source": body.get("source"),
                "configured": body.get("configured"), "total": body.get("total"),
                "failed": body.get("failed"), "items": len(body.get("items", [])),
                "resource_ids": [item.get("id") or item.get("transaction_id") for item in body.get("items", [])],
                "trace_ids": [item.get("trace_id") for item in body.get("items", []) if item.get("trace_id")],
                "series_points": sum(len(series.get("points", [])) for series in body.get("series", []))}
    except RuntimeError as error:
        return {"error": str(error)}


def run():
    global RUN_ID
    _, mode = api("/auth/status")
    if mode.get("required"):
        raise RuntimeError("this exercise requires local demo mode; do not bypass protected authentication")
    if any(a["state"] == "firing" for a in prometheus_alerts()):
        raise RuntimeError("development already has a firing business-success alert")

    baseline = purchase("SUCCESS")
    EVIDENCE["baseline_transaction"] = baseline["id"]
    print("Healthy baseline purchase", baseline["id"], flush=True)

    command = {"environment": ENVIRONMENT, "scenario": "payment-decline", "percentage": 100,
               "duration_seconds": 900, "reason": "Bounded live ITOC failure-to-recovery verification"}
    _, run = api("/simulations", command, key=str(uuid.uuid4()))
    RUN_ID = run["id"]
    EVIDENCE["simulation_run"] = RUN_ID
    print("Started controlled payment decline", RUN_ID, flush=True)

    failures = [purchase("FAILED") for _ in range(6)]
    if any(p.get("error_code") != "SIMULATED_PAYMENT_DECLINED" for p in failures):
        raise RuntimeError("a failed purchase did not carry the injected payment decline")
    EVIDENCE["failed_transactions"] = [p["id"] for p in failures]
    EVIDENCE["failed_trace_ids"] = [p["trace_id"] for p in failures]
    print("Six business failures recorded, with trace IDs", flush=True)

    alert = wait_for("firing development business-success alert",
                     lambda: next((a for a in prometheus_alerts() if a["state"] == "firing"), None), 180)
    EVIDENCE["alert_active_at"] = alert["activeAt"]
    print("Prometheus alert firing since", alert["activeAt"], flush=True)

    def matching_incident():
        _, page = api("/incidents?" + urllib.parse.urlencode({"environment": ENVIRONMENT, "search": "BusinessSuccessRateLow", "limit": 50}))
        for item in page["items"]:
            if abs((iso(item["detected_at"]) - iso(alert["activeAt"])).total_seconds()) < 1:
                return item
        return None

    found = wait_for("incident created from exact alert episode", matching_incident, 120)
    incident_id = found["id"]
    EVIDENCE["incident_id"] = incident_id
    _, detail = api("/incidents/" + incident_id)
    incident = detail["incident"]
    if incident["state"] != "Detected":
        raise RuntimeError("new alert incident was unexpectedly modified")
    print("Incident created", incident_id, flush=True)

    EVIDENCE["purchase_impact"] = optional_evidence("/incidents/" + incident_id + "/purchase-impact?window=2h")
    EVIDENCE["metrics_evidence"] = optional_evidence("/incidents/" + incident_id + "/metrics?window=2h")
    def correlated_traces():
        evidence = optional_evidence("/incidents/" + incident_id + "/traces?window=2h")
        return evidence if set(EVIDENCE["failed_trace_ids"]) & set(evidence.get("trace_ids", [])) else None

    EVIDENCE["trace_evidence"] = wait_for("Jaeger trace from the injected failure", correlated_traces, 90)
    EVIDENCE["log_evidence"] = optional_evidence("/incidents/" + incident_id + "/logs?window=2h")
    EVIDENCE["failed_purchase_correlated"] = bool(set(EVIDENCE["failed_transactions"]) & set(EVIDENCE["purchase_impact"].get("resource_ids", [])))
    EVIDENCE["failed_trace_correlated"] = bool(set(EVIDENCE["failed_trace_ids"]) & set(EVIDENCE["trace_evidence"].get("trace_ids", [])))
    if not EVIDENCE["failed_purchase_correlated"] or not EVIDENCE["metrics_evidence"].get("series_points"):
        raise RuntimeError("purchase impact or measured metric evidence is missing")
    print("Investigation routes reviewed", flush=True)

    fields = {key: incident[key] for key in ("title", "severity", "owner", "impact", "root_cause", "mitigation", "resolution", "postmortem_notes", "related_deployment", "affected_transactions", "affected_users", "error_rate", "success_rate", "latency_ms", "evidence", "action_items") if key in incident}
    fields.update({"owner": "payments-oncall", "impact": "Six observed synthetic package purchases failed in development during the injected payment decline.",
                   "affected_transactions": len(failures), "action_items": [
                       {"title": "Review payment failure guardrail and rollback drill", "owner": "payments-platform", "priority": "P1", "status": "Open", "done": False}
                   ]})
    incident = update_incident(incident, "Acknowledged", fields, "On-call accepted Prometheus business-success alert")
    incident = update_incident(incident, "Investigating", fields, "Compared failed purchase IDs with metrics and traces")
    _, incident = api("/incidents/" + incident_id + "/escalations", {
        "expected_version": incident["version"], "team": "Payments Platform", "owner": "payments-oncall",
        "reason": "Six observed synthetic purchase failures require coordinated payment mitigation"})
    print("Recorded escalation to Payments Platform", flush=True)
    fields["root_cause"] = "Controlled payment-decline simulation intercepted payment reserve for new synthetic purchases."
    incident = update_incident(incident, "Identified", fields, "Matched failure code and simulation run to the payment dependency")
    fields["mitigation"] = "Stopped the controlled payment-decline run, then checked fresh package purchases."
    incident = update_incident(incident, "Mitigating", fields, "Approved simulation stop as mitigation")
    stop_run()
    recovered = purchase("SUCCESS")
    EVIDENCE["first_recovered_transaction"] = recovered["id"]
    incident = update_incident(incident, "Monitoring", fields, "Fresh synthetic purchase succeeded after simulation stop")
    EVIDENCE["monitoring_at"] = incident["updated_at"]
    _, early = api("/incidents/" + incident_id + "/recovery-assessment")
    if early["status"] != "collecting":
        raise RuntimeError("new Monitoring incident should still be collecting a sustained sample")
    finish_monitoring(incident)


def finish_monitoring(incident):
    incident_id = incident["id"]
    if incident["state"] != "Monitoring":
        raise RuntimeError("recovery exercise requires a Monitoring incident")
    # Host sleep can make a preplanned batch fall outside the rolling windows.
    # Feed a small steady stream until the server itself accepts both windows.
    deadline = time.monotonic() + 3600
    recovered_ids = EVIDENCE.setdefault("monitoring_transactions", [])
    while time.monotonic() < deadline:
        recovered_ids.append(purchase("SUCCESS")["id"])
        _, assessment = api("/incidents/" + incident_id + "/recovery-assessment")
        EVIDENCE["recovery_assessment"] = assessment
        print("Recovery", assessment["status"], "earlier", assessment["earlier_total"],
              "recent", assessment["recent_total"], flush=True)
        if assessment["status"] == "meets_target":
            break
        if assessment["status"] not in ("collecting", "insufficient_traffic"):
            raise RuntimeError("recovery assessment rejected healthy stream: " + assessment["status"])
        time.sleep(30)
    else:
        raise RuntimeError("live two-window recovery sample did not meet target within one hour")
    print("Live two-window recovery sample meets target:", assessment["earlier_total"], assessment["recent_total"], flush=True)

    fields = {key: incident[key] for key in ("title", "severity", "owner", "impact", "root_cause", "mitigation", "resolution", "postmortem_notes", "related_deployment", "affected_transactions", "affected_users", "error_rate", "success_rate", "latency_ms", "evidence", "action_items") if key in incident}
    fields["resolution"] = "New synthetic purchases succeeded throughout two observed five-minute windows after the controlled fault stopped."
    validation = {"observation": f"Observed {assessment['earlier_success']}/{assessment['earlier_total']} and {assessment['recent_success']}/{assessment['recent_total']} successful synthetic purchases in adjacent five-minute windows.",
                  "source_url": BASE + "/api/v1/incidents/" + incident_id + "/recovery-assessment",
                  "observed_at": datetime.now(timezone.utc).isoformat()}
    incident = update_incident(incident, "Resolved", fields, "Reviewed sustained purchase sample and resolved synthetic incident", validation)
    _, report = api("/incidents/" + incident_id + "/postmortem", {
        "expected_version": incident["version"], "summary": "Controlled payment decline and verified synthetic purchase recovery",
        "detection": "Prometheus business-success burn alert created the incident after six failed purchases.",
        "contributing_factors": "A 100% payment-decline simulation was deliberately active for development purchases.",
        "what_went_well": "Transaction IDs, trace IDs, metric counts and incident audit supported investigation and recovery review.",
        "what_went_wrong": "External Splunk and Dynatrace ingestion remain unconfigured in this local exercise.",
        "note": "Recorded the controlled end-to-end learning review"})
    if report["incident_id"] != incident_id or not report.get("action_items"):
        raise RuntimeError("postmortem did not freeze the incident and corrective action")
    _, final = api("/incidents/" + incident_id)
    if final["incident"]["state"] != "Postmortem" or not final["postmortem"]["recovery_validation"]:
        raise RuntimeError("postmortem state or recovery record missing")
    _, audit = api("/audit?" + urllib.parse.urlencode({"environment": ENVIRONMENT, "source": "incident", "resource": incident_id, "limit": 25}))
    actions = [row["action"] for row in audit["items"]]
    if len(actions) < 9 or "escalated" not in actions or "postmortem_generated" not in actions:
        raise RuntimeError("incident lifecycle absent from operations audit")
    EVIDENCE["audit_actions"] = actions
    EVIDENCE["final_state"] = final["incident"]["state"]
    EVIDENCE["finished_at"] = datetime.now(timezone.utc).isoformat()
    print("Resolved, postmortem generated, and audit history verified", flush=True)


def resume():
    global EVIDENCE, RUN_ID, RUN_STOPPED
    path = Path("/private/tmp/telcopulse-live-incident-demo.json")
    EVIDENCE = json.loads(path.read_text())
    RUN_ID = EVIDENCE["simulation_run"]
    _, run_state = api("/simulations/" + RUN_ID)
    if run_state.get("run", {}).get("active") is not False:
        raise RuntimeError("cannot resume while the controlled fault is active")
    RUN_STOPPED = True
    _, detail = api("/incidents/" + EVIDENCE["incident_id"])
    print("Resuming recovered incident", EVIDENCE["incident_id"], flush=True)
    EVIDENCE["trace_evidence"] = optional_evidence("/incidents/" + EVIDENCE["incident_id"] + "/traces?window=2h")
    EVIDENCE["failed_trace_correlated"] = bool(set(EVIDENCE["failed_trace_ids"]) & set(EVIDENCE["trace_evidence"].get("trace_ids", [])))
    finish_monitoring(detail["incident"])


if __name__ == "__main__":
    try:
        resume() if "--resume" in sys.argv[1:] else run()
    finally:
        try:
            stop_run()
        except Exception as error:
            EVIDENCE["stop_error"] = str(error)
            print("Simulation stop needs attention:", error, flush=True)
        path = Path("/private/tmp/telcopulse-live-incident-demo.json")
        path.write_text(json.dumps(EVIDENCE, indent=2, sort_keys=True) + "\n")
        print("Evidence written to", path, flush=True)
