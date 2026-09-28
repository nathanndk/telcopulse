"""Verify the internal local auth foundation without printing credentials or tokens."""

import hashlib
import json
import os
from pathlib import Path
import re
import subprocess


ROOT = Path(__file__).resolve().parents[2]
os.environ.setdefault("WEB_PORT", "3001")
PASSWORD = (ROOT / ".secrets/local-admin-password").read_text().strip()


def auth(action, payload, authorized=True):
    assert action in {"login", "validate", "logout"}
    header = '--header="Authorization: Bearer $SERVICE_TOKEN" ' if authorized else ""
    command = (
        "wget -q -S -O - " + header +
        '--header="Content-Type: application/json" --post-file=/dev/stdin '
        "http://127.0.0.1:8080/internal/auth/" + action
    )
    result = subprocess.run(
        ["docker", "compose", "exec", "-T", "auth-service", "sh", "-c", command],
        input=json.dumps(payload), text=True, capture_output=True, timeout=15, cwd=ROOT,
    )
    match = re.search(r"HTTP/1\.1 (\d{3})", result.stderr)
    assert match, result.stderr
    return int(match.group(1)), json.loads(result.stdout) if result.stdout else None


def sql(statement):
    result = subprocess.run(
        ["docker", "compose", "exec", "-T", "postgres", "psql", "-X", "-q", "-t", "-A", "-U", "telcopulse", "-d", "telcopulse", "-c", statement],
        text=True, capture_output=True, timeout=10, cwd=ROOT,
    )
    if result.returncode:
        raise AssertionError(result.stderr)
    return result.stdout.strip()


assert sql("SELECT count(*) FROM auth.users WHERE username='local-admin' AND role='Administrator' AND active=true") == "1"
assert auth("login", {"username": "local-admin", "password": "wrong"})[0] == 401
assert auth("login", {"username": "unknown-operator", "password": "wrong"})[0] == 401
assert auth("login", {"username": "local-admin", "password": PASSWORD}, authorized=False)[0] == 401
try:
    sql("UPDATE auth.events SET action='logout' WHERE sequence=(SELECT min(sequence) FROM auth.events)")
except AssertionError as error:
    assert "append-only" in str(error)
else:
    raise AssertionError("authentication audit history was mutable")
status, session = auth("login", {"username": "local-admin", "password": PASSWORD})
assert status == 200 and session["user"]["role"] == "Administrator"
token = session["token"]
assert len(token) == 64
stored = sql("SELECT encode(token_hash,'hex') FROM auth.sessions ORDER BY created_at DESC LIMIT 1")
assert stored == hashlib.sha256(token.encode()).hexdigest() and stored != token
assert auth("validate", {"token": token})[1]["role"] == "Administrator"

try:
    sql("UPDATE auth.users SET role='Viewer' WHERE username='local-admin'")
    assert auth("validate", {"token": token})[1]["role"] == "Viewer"
    sql("UPDATE auth.users SET active=false WHERE username='local-admin'")
    assert auth("validate", {"token": token})[0] == 401
finally:
    sql("UPDATE auth.users SET role='Administrator',active=true WHERE username='local-admin'")

assert auth("validate", {"token": token})[0] == 200
assert auth("logout", {"token": token})[0] == 200
assert auth("logout", {"token": token})[0] == 200
assert auth("validate", {"token": token})[0] == 401

try:
    for _ in range(5):
        assert auth("login", {"username": "local-admin", "password": "wrong"})[0] == 401
    assert sql("SELECT failed_attempts FROM auth.users WHERE username='local-admin'") == "5"
    assert auth("login", {"username": "local-admin", "password": PASSWORD})[0] == 401
finally:
    sql("UPDATE auth.users SET failed_attempts=0,locked_until=NULL WHERE username='local-admin'")

status, again = auth("login", {"username": "local-admin", "password": PASSWORD})
assert status == 200
auth("logout", {"token": again["token"]})
print(json.dumps({
    "bootstrap_admins": 1,
    "verified": ["generic invalid login", "internal service token", "append-only login audit", "hashed opaque session", "live role and active checks", "logout revocation", "five-attempt lockout", "account recovery"],
}))
