"""Exercise administrator user/role management through the internal auth API."""

import json
import os
from pathlib import Path
import re
import secrets
import subprocess


ROOT = Path(__file__).resolve().parents[2]
os.environ.setdefault("WEB_PORT", "3001")
ADMIN_PASSWORD = (ROOT / ".secrets/local-admin-password").read_text().strip()


def auth(path, payload):
    assert re.fullmatch(r"[A-Za-z/0-9-]+", path)
    command = (
        'wget -q -S -O - --header="Authorization: Bearer $SERVICE_TOKEN" '
        '--header="Content-Type: application/json" --post-file=/dev/stdin '
        "http://127.0.0.1:8080/internal/auth/" + path
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


status, session = auth("login", {"username": "local-admin", "password": ADMIN_PASSWORD})
assert status == 200
admin_token = session["token"]
admin_id = session["user"]["id"]
username = "managed" + secrets.token_hex(4)
password = secrets.token_urlsafe(24)
subject_id = None
try:
    status, created = auth("users/create", {"token": admin_token, "user": {"username": username, "password": password, "role": "Viewer"}})
    assert status == 201 and created["role"] == "Viewer" and created["active"]
    assert "password" not in created and "password_hash" not in created
    subject_id = created["id"]
    assert auth("users/create", {"token": admin_token, "user": {"username": username, "password": password, "role": "Viewer"}})[0] == 409
    status, page = auth("users/list", {"token": admin_token, "cursor": ""})
    assert status == 200 and any(item["id"] == subject_id for item in page["items"])

    status, viewer = auth("login", {"username": username, "password": password})
    assert status == 200 and viewer["user"]["role"] == "Viewer"
    viewer_token = viewer["token"]
    assert auth("users/list", {"token": viewer_token, "cursor": ""})[0] == 403
    assert auth("users/create", {"token": viewer_token, "user": {"username": "forbidden", "password": password, "role": "Administrator"}})[0] == 403

    path = "users/" + subject_id + "/update"
    status, changed = auth(path, {"token": admin_token, "change": {"role": "Operator"}})
    assert status == 200 and changed["role"] == "Operator"
    assert auth("validate", {"token": viewer_token})[1]["role"] == "Operator"
    assert auth("users/" + admin_id + "/update", {"token": admin_token, "change": {"role": "Viewer"}})[0] == 409
    assert auth("validate", {"token": admin_token})[1]["role"] == "Administrator"

    status, disabled = auth(path, {"token": admin_token, "change": {"active": False}})
    assert status == 200 and not disabled["active"]
    assert auth("validate", {"token": viewer_token})[0] == 401
    actions = sql("SELECT string_agg(action,',' ORDER BY sequence) FROM auth.events WHERE user_id='" + subject_id + "' AND action IN ('user_created','role_changed','user_deactivated')")
    assert actions == "user_created,role_changed,user_deactivated"
    actors = sql("SELECT count(*) FROM auth.events WHERE user_id='" + subject_id + "' AND actor_user_id='" + admin_id + "' AND action IN ('user_created','role_changed','user_deactivated')")
    assert actors == "3"
finally:
    if subject_id:
        auth("users/" + subject_id + "/update", {"token": admin_token, "change": {"active": False}})
    auth("logout", {"token": admin_token})

print(json.dumps({"verified": ["administrator create/list/update", "viewer denial", "current role", "session revocation", "last administrator guard", "append-only actor audit"]}))
