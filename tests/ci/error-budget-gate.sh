#!/usr/bin/env bash
# Contract test for scripts/error-budget-gate.sh against a local fake Prometheus
# query API. Needs bash, curl, jq and python3; contacts no cluster.
set -euo pipefail
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
temp_dir=$(mktemp -d)
trap 'kill "$server_pid" 2>/dev/null || true; rm -rf "$temp_dir"' EXIT

cat > "$temp_dir/server.py" <<'PY'
import json, os, sys, urllib.parse, http.server
state_file = sys.argv[1]
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        state = json.load(open(state_file))
        if state.get("down"):
            self.send_response(503); self.end_headers(); return
        query = urllib.parse.parse_qs(urllib.parse.urlparse(self.path).query)["query"][0]
        if query.startswith("ALERTS"):
            result = state["alerts"]
        elif query.startswith("slo:objective"):
            result = state.get("objectives", [{"metric": {"sloth_slo": n}, "value": [0, "0.99"]} for n in ("purchase-success", "purchase-latency", "notification-freshness")])
        else:
            result = state["budgets"]
        body = json.dumps({"status": "success", "data": {"resultType": "vector", "result": result}}).encode()
        self.send_response(200); self.send_header("Content-Type", "application/json"); self.end_headers(); self.wfile.write(body)
    def log_message(self, *a): pass
server = http.server.HTTPServer(("127.0.0.1", 0), H)
print(server.server_port, flush=True)
server.serve_forever()
PY
state="$temp_dir/state.json"
echo '{}' > "$state"
python3 "$temp_dir/server.py" "$state" > "$temp_dir/port" &
server_pid=$!
for _ in $(seq 50); do [[ -s "$temp_dir/port" ]] && break; sleep 0.1; done
export SLO_PROMETHEUS_URL="http://127.0.0.1:$(cat "$temp_dir/port")"

budget() { printf '{"metric":{"sloth_slo":"%s","environment":"staging"},"value":[0,"%s"]}' "$1" "$2"; }
set_state() { printf '{"alerts":[%s],"budgets":[%s]%s}' "$1" "$2" "${3:-}" > "$state"; }
expect() { # outcome target [env...]
  local outcome=$1 target=$2; shift 2
  if env TARGET="$target" "$@" bash "$repo_root/scripts/error-budget-gate.sh" > "$temp_dir/out" 2>&1; then got=pass; else got=block; fi
  [[ "$got" == "$outcome" ]] || { echo "expected $outcome for $target, got $got:" >&2; cat "$temp_dir/out" >&2; exit 1; }
}

healthy="$(budget purchase-success 0.42),$(budget purchase-latency 0.9),$(budget notification-freshness NaN)"
set_state '' "$healthy"
expect pass STAGING
grep -q 'purchase-success (staging): 42.0% of error budget remaining' "$temp_dir/out"
grep -q 'notification-freshness (staging): budget unknown' "$temp_dir/out"

set_state '{"metric":{"alertname":"PurchaseLatencyBudgetBurn","environment":"staging"},"value":[0,"1"]}' "$healthy"
expect block STAGING
grep -q 'burn alert firing: PurchaseLatencyBudgetBurn' "$temp_dir/out"
expect block PRODUCTION_LIKE
expect pass DEV
expect block STAGING BUDGET_OVERRIDE_REASON='too short'
expect pass STAGING BUDGET_OVERRIDE_REASON='INC-42 rollback-safe fix for payment timeout regression'
grep -q 'ERROR BUDGET OVERRIDE for STAGING' "$temp_dir/out"

set_state '' "$(budget purchase-success -0.3),$(budget purchase-latency 0.9)"
expect block STAGING
grep -q 'purchase-success error budget exhausted' "$temp_dir/out"

set_state '' "$(budget purchase-success 0.5)"
expect pass STAGING
grep -q 'purchase-latency (staging): budget unknown' "$temp_dir/out"

set_state '' '' ',"objectives":[]'
expect block STAGING
grep -q 'no SLO recording rules loaded in Prometheus' "$temp_dir/out"

set_state '' "$healthy" ',"down":true'
expect block STAGING
grep -q 'refusing to promote without SLO evidence' "$temp_dir/out"

expect block STAGING SLO_PROMETHEUS_URL='http://user:pass@prometheus:9090'
expect block UNKNOWN
echo 'error budget gate contract: ok'
