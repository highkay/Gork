#!/usr/bin/env python3
"""Offline SSO local precheck + cleanup via Admin API.

Mirrors protocol.SSOLocalInvalidReason (no HTTP):
  - empty cookie          → empty
  - non-JWT (< 2 dots)    → keep (needs online probe)
  - JWT exp < now - 60s   → jwt_expired

Also can delete status=expired accounts.

Usage:
  docker cp gork:/app/data/accounts.db /tmp/accounts.db
  python3 scripts/sso_offline_cleanup.py /tmp/accounts.db --dry-run
  python3 scripts/sso_offline_cleanup.py /tmp/accounts.db --auth gork
"""

from __future__ import annotations

import argparse
import base64
import json
import sqlite3
import sys
import time
import urllib.error
import urllib.request
from collections import Counter
from pathlib import Path


def api_json(method: str, url: str, payload, auth: str, timeout: float = 120.0):
    data = None if payload is None else json.dumps(payload).encode()
    req = urllib.request.Request(
        url,
        data=data,
        headers={
            "Authorization": f"Bearer {auth}",
            "Content-Type": "application/json",
        },
        method=method,
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read().decode()
            return resp.status, json.loads(raw) if raw else {}
    except urllib.error.HTTPError as exc:
        body = exc.read().decode(errors="replace")
        raise RuntimeError(f"HTTP {exc.code}: {body[:400]}") from exc


def batched(items: list[str], size: int):
    for i in range(0, len(items), size):
        yield items[i : i + size]


def extract_sso_value(token: str) -> str:
    raw = (token or "").strip()
    if not raw:
        return ""
    lower = raw.lower()
    if "sso=" in lower or "sso-rw=" in lower:
        for part in raw.split(";"):
            part = part.strip()
            if "=" not in part:
                continue
            name, value = part.split("=", 1)
            name = name.strip().lower()
            value = value.strip()
            if name in ("sso", "sso-rw") and value:
                return value
        return ""
    return raw


def b64url_decode(segment: str) -> bytes:
    segment = segment.strip()
    pad = "=" * (-len(segment) % 4)
    return base64.urlsafe_b64decode(segment + pad)


def sso_local_invalid_reason(token: str, now: float) -> str:
    value = extract_sso_value(token)
    if not value:
        return "empty"
    parts = value.split(".")
    if len(parts) < 3:
        return ""  # non-JWT: do not kill offline
    try:
        payload = json.loads(b64url_decode(parts[1]))
    except Exception:
        return ""
    exp = payload.get("exp")
    if not isinstance(exp, (int, float)) or exp <= 0:
        return ""
    if int(exp) < int(now) - 60:
        return "jwt_expired"
    return ""


def scan_db(path: Path, now: float) -> dict:
    conn = sqlite3.connect(f"file:{path}?mode=ro", uri=True)
    conn.row_factory = sqlite3.Row
    status = dict(conn.execute("SELECT status, COUNT(*) FROM accounts GROUP BY status").fetchall())
    expired = [
        r[0]
        for r in conn.execute(
            "SELECT token FROM accounts WHERE status = 'expired' AND deleted_at IS NULL"
        )
    ]
    # also already soft-deleted?
    deleted = conn.execute(
        "SELECT COUNT(*) FROM accounts WHERE deleted_at IS NOT NULL"
    ).fetchone()[0]

    local_reasons: Counter[str] = Counter()
    local_dead: list[tuple[str, str]] = []  # token, reason
    non_jwt = 0
    jwt_ok = 0
    active_total = 0

    for row in conn.execute(
        "SELECT token, status FROM accounts WHERE status = 'active' AND deleted_at IS NULL"
    ):
        active_total += 1
        token = row["token"]
        reason = sso_local_invalid_reason(token, now)
        if reason:
            local_reasons[reason] += 1
            local_dead.append((token, reason))
        else:
            value = extract_sso_value(token)
            if value.count(".") < 2:
                non_jwt += 1
            else:
                jwt_ok += 1

    conn.close()
    return {
        "status": status,
        "deleted_at_set": deleted,
        "expired_tokens": expired,
        "local_dead": local_dead,
        "local_reasons": dict(local_reasons),
        "active_total": active_total,
        "active_jwt_ok": jwt_ok,
        "active_non_jwt": non_jwt,
    }


def delete_tokens(base: str, auth: str, tokens: list[str], batch: int, dry_run: bool) -> int:
    if not tokens:
        return 0
    if dry_run:
        print(f"[dry-run] would DELETE {len(tokens)} tokens")
        return 0
    deleted = 0
    for i, chunk in enumerate(batched(tokens, batch), 1):
        status, body = api_json("DELETE", f"{base}/admin/api/tokens", chunk, auth)
        n = int(body.get("deleted", len(chunk)))
        deleted += n
        if i % 10 == 0 or i == 1:
            print(f"delete batch {i}: http={status} deleted_batch={n} total={deleted}")
    return deleted


def main() -> int:
    parser = argparse.ArgumentParser(description="Offline SSO local cleanup")
    parser.add_argument("db", nargs="?", default="/tmp/accounts.db")
    parser.add_argument("--base-url", default="http://127.0.0.1:8008")
    parser.add_argument("--auth", default="gork")
    parser.add_argument("--batch-size", type=int, default=200)
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--skip-expired", action="store_true")
    parser.add_argument("--skip-local-dead", action="store_true")
    parser.add_argument(
        "--report",
        default="",
        help="optional path to write deleted tokens report jsonl",
    )
    args = parser.parse_args()

    path = Path(args.db)
    if not path.exists():
        print(f"db not found: {path}", file=sys.stderr)
        return 1

    now = time.time()
    scan = scan_db(path, now)
    local_tokens = [t for t, _ in scan["local_dead"]]
    expired_tokens = [] if args.skip_expired else scan["expired_tokens"]
    if args.skip_local_dead:
        local_tokens = []

    # de-dupe preserve order
    seen: set[str] = set()
    to_delete: list[str] = []
    for tok in expired_tokens + local_tokens:
        if tok not in seen:
            seen.add(tok)
            to_delete.append(tok)

    print("=== offline SSO local cleanup plan ===")
    print(f"db: {path}")
    print(f"status: {scan['status']} deleted_at_set={scan['deleted_at_set']}")
    print(
        f"active={scan['active_total']} jwt_ok={scan['active_jwt_ok']} "
        f"non_jwt={scan['active_non_jwt']} local_dead={len(scan['local_dead'])} "
        f"reasons={scan['local_reasons']}"
    )
    print(
        f"plan: delete_expired={len(expired_tokens)} delete_local_dead={len(local_tokens)} "
        f"unique_total={len(to_delete)} dry_run={args.dry_run}"
    )

    if args.report:
        report_path = Path(args.report)
        with report_path.open("w", encoding="utf-8") as fh:
            for tok, reason in scan["local_dead"]:
                fh.write(json.dumps({"token_prefix": tok[:12], "reason": reason, "kind": "local"}) + "\n")
            for tok in expired_tokens:
                fh.write(json.dumps({"token_prefix": tok[:12], "reason": "expired", "kind": "status"}) + "\n")
        print(f"report: {report_path}")

    if args.dry_run:
        print("dry-run only, no changes")
        return 0

    # verify admin first
    st, body = api_json("GET", f"{args.base_url.rstrip('/')}/admin/api/verify", None, args.auth)
    if st != 200:
        print(f"admin verify failed: {st} {body}", file=sys.stderr)
        return 1

    deleted = delete_tokens(
        args.base_url.rstrip("/"), args.auth, to_delete, args.batch_size, False
    )
    print(f"done: deleted={deleted} requested={len(to_delete)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
