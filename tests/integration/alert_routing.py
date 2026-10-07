"""Route a real outage through Alertmanager to an incident and a Telegram page.

A local mock of the Telegram Bot API stands in for api.telegram.org, so the
rendered request (bot token path, chat ID, message) is checked without sending
anything outside this machine. The script stops one service for about a
minute, silences a second outage, and restores the default Alertmanager
configuration, both services and the mock in finally blocks. Local stack only.
"""
import json
import os
import subprocess
import time
import urllib.request
from kafka_recovery import BASE_URL, PROMETHEUS_URL

ALERTMANAGER_URL = os.environ.get("ALERTMANAGER_URL", "http://localhost:9093")
OVERLAY = ["-f", "compose.yaml", "-f", "infrastructure/alertmanager/telegram.compose.yaml"]
MOCK = "telegram-mock"
MOCK_SERVER = r'''
import json, http.server
messages = []
class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", 0)))
        messages.append({"path": self.path, "body": json.loads(body or b"{}")})
        self.send_response(200); self.send_header("Content-Type", "application/json"); self.end_headers()
        self.wfile.write(json.dumps({"ok": True, "result": {"message_id": len(messages), "date": 0, "chat": {"id": 1, "type": "group"}}}).encode())
    def do_GET(self):
        self.send_response(200); self.send_header("Content-Type", "application/json"); self.end_headers()
        self.wfile.write(json.dumps(messages).encode())
    def log_message(self, *args): pass
http.server.ThreadingHTTPServer(("0.0.0.0", 8080), H).serve_forever()
'''


def run(*args, env=None):
    return subprocess.check_output(list(args), text=True, env=env).strip()


def get_json(url):
    with urllib.request.urlopen(url, timeout=5) as response:
        return json.load(response)


def post_json(url, body):
    request = urllib.request.Request(url, data=json.dumps(body).encode(), headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(request, timeout=5) as response:
        return json.load(response)


def telegram_messages():
    out = run("docker", "exec", MOCK, "wget", "-qO-", "http://127.0.0.1:8080/messages")
    return json.loads(out)


def incidents(title):
    items, cursor = [], ""
    while True:
        page = get_json(BASE_URL + "/api/v1/incidents?environment=development&limit=100&cursor=" + cursor)
        items.extend(page["items"])
        if not page["more"]:
            return [i for i in items if i["title"] == title]
        cursor = page["next"]


def wait(predicate, seconds, message):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        value = predicate()
        if value:
            return value
        time.sleep(2)
    raise AssertionError(message)


network = next(n for n in run("docker", "network", "ls", "--format", "{{.Name}}").splitlines() if n.endswith("_frontend") and n.startswith("telcopulse"))
# Compose reads secret values from these variables; nothing is written to disk.
env = dict(os.environ, TELEGRAM_API_URL="http://telegram-mock:8080", TELEGRAM_BOT_TOKEN="123456:local-test-token", TELEGRAM_CHAT_ID="-1001234567890")
silence_id = None
before_payment = {i["id"] for i in incidents("ServiceUnavailable · payment-service")}
before_package = {i["id"] for i in incidents("ServiceUnavailable · package-service")}
run("docker", "run", "-d", "--rm", "--name", MOCK, "--network", network, "python:3.14-alpine", "python", "-c", MOCK_SERVER)
try:
    run("docker", "compose", *OVERLAY, "up", "-d", "--wait", "alertmanager", env=env)
    logs = run("docker", "compose", "logs", "alertmanager")
    assert "Telegram notification enabled" in logs, logs[-2000:]
    # A maintenance silence suppresses both the page and incident creation.
    silence_id = post_json(ALERTMANAGER_URL + "/api/v2/silences", {
        "matchers": [{"name": "alertname", "value": "ServiceUnavailable", "isRegex": False}, {"name": "service", "value": "package-service", "isRegex": False}],
        "startsAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "endsAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() + 900)),
        "createdBy": "tests/integration/alert_routing.py",
        "comment": "Planned package-service maintenance during routing test",
    })["silenceID"]
    run("docker", "compose", "stop", "payment-service", "package-service")
    try:
        created = wait(lambda: [i for i in incidents("ServiceUnavailable · payment-service") if i["id"] not in before_payment], 150, "No incident from the Alertmanager webhook")
        assert len(created) == 1, created
        page = wait(lambda: [m for m in telegram_messages() if "ServiceUnavailable" in m["body"].get("text", "") and "payment-service" in m["body"]["text"]], 60, "No Telegram page for the outage")
        message = page[0]
        assert message["path"] == "/bot123456:local-test-token/sendMessage", message["path"]
        assert str(message["body"]["chat_id"]) == "-1001234567890", message["body"]
        assert message["body"]["text"].startswith("🔴 PAGE SEV-1 · ServiceUnavailable"), message["body"]["text"]
        assert "runbook: docs/runbooks/pod-crashloopbackoff.md" in message["body"]["text"]
        silenced = [a for a in get_json(ALERTMANAGER_URL + "/api/v2/alerts?silenced=true&active=false") if a["labels"].get("service") == "package-service"]
        assert silenced and silenced[0]["status"]["silencedBy"] == [silence_id], silenced
        assert not any("package-service" in m["body"].get("text", "") for m in telegram_messages()), "Silenced outage was paged"
        assert [i for i in incidents("ServiceUnavailable · package-service") if i["id"] not in before_package] == [], "Silenced outage opened an incident"
        print(json.dumps({"incident_id": created[0]["id"], "telegram_page": True, "silence_respected": True}), flush=True)
    finally:
        run("docker", "compose", "start", "payment-service", "package-service")
    wait(lambda: [m for m in telegram_messages() if m["body"].get("text", "").startswith("✅ RESOLVED") and "payment-service" in m["body"]["text"]], 240, "No resolved notification after recovery")
    detail = get_json(BASE_URL + "/api/v1/incidents/" + created[0]["id"])
    assert detail["incident"]["state"] == "Detected" and len(detail["history"]) == 1, "Resolution must stay an operator decision"
    print(json.dumps({"resolved_notification": True, "incident_state": detail["incident"]["state"]}), flush=True)
finally:
    if silence_id:
        urllib.request.urlopen(urllib.request.Request(ALERTMANAGER_URL + "/api/v2/silence/" + silence_id, method="DELETE"), timeout=5).close()
    subprocess.run(["docker", "rm", "-f", MOCK], capture_output=True)
    subprocess.run(["docker", "compose", "up", "-d", "--wait", "alertmanager"], check=True)
