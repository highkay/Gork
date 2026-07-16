#!/usr/bin/env python3
"""Clean bad gork accounts via Admin API.

Default (safe):
  - delete status=expired accounts

Optional:
  --disable-sso-soft-fail   disable active accounts whose last SSO stage failed
                           with no last_ok_at (soft fail, not blocked-user)
  --delete-never-used-failed
                           delete active accounts with usage_fail_count>0 and
                           usage_use_count=0 AND last_fail_reason=invalid_credentials
                           (usually already expired; kept for completeness)

Usage:
  docker cp gork:/app/data/accounts.db /tmp/accounts.db
  python3 scripts/cleanup_bad_accounts.py /tmp/accounts.db --dry-run
  python3 scripts/cleanup_bad_accounts.py /tmp/accounts.db
  python3 scripts/cleanup_bad_accounts.py /tmp/accounts.db --disable-sso-soft-fail
"""

from __future__ import annotations

import argparse
import json
import sqlite3
import sys
import urllib.error
import urllib.request
from pathlib import Path


def api_json(method: str, url: str, payload, auth: str, timeout: float = 60.0):
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
        raise RuntimeError(f"HTTP {exc.code}: {body[:300]}") from exc


def batched(items: list[str], size: int):
    for i in range(0, len(items), size):
        yield items[i : i + size]


def load_expired(conn: sqlite3.Connection) -> list[str]:
    return [r[0] for r in conn.execute("SELECT token FROM accounts WHERE status = 'expired'")]


def load_sso_soft_fail_never_ok(conn: sqlite3.Connection) -> list[str]:
    out: list[str] = []
    for token, ext in conn.execute(
        "SELECT token, ext FROM accounts WHERE status = 'active' AND ext IS NOT NULL"
    ):
        try:
            data = json.loads(ext)
        except json.JSONDecodeError:
            continue
        sv = data.get("sso_validation")
        if not isinstance(sv, dict):
            continue
        if int(sv.get("failure_count") or 0) <= 0:
            continue
        if sv.get("last_ok_at"):
            continue
        # keep network/timeout failures out if reason looks transient
        reason = str(sv.get("last_fail_reason") or "").lower()
        if "timeout" in reason or "eof" in reason or "connection" in reason:
            continue
        out.append(token)
    return out


def load_never_used_invalid(conn: sqlite3.Connection) -> list[str]:
    return [
        r[0]
        for r in conn.execute(
            """
            SELECT token FROM accounts
            WHERE status = 'active'
              AND COALESCE(usage_use_count, 0) = 0
              AND COALESCE(usage_fail_count, 0) > 0
              AND last_fail_reason = 'invalid_credentials'
            """
        )
    ]


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
        print(f"delete batch {i}: http={status} deleted={n} body={body}")
    return deleted


def disable_tokens(base: str, auth: str, tokens: list[str], batch: int, dry_run: bool) -> int:
    if not tokens:
        return 0
    if dry_run:
        print(f"[dry-run] would DISABLE {len(tokens)} tokens")
        return 0
    done = 0
    for i, chunk in enumerate(batched(tokens, batch), 1):
        payload = {"tokens": chunk, "disabled": True}
        status, body = api_json(
            "POST", f"{base}/admin/api/tokens/disabled/batch", payload, auth
        )
        done += len(chunk)
        print(f"disable batch {i}: http={status} body={body}")
    return done


def main() -> int:
    parser = argparse.ArgumentParser(description="Clean bad gork accounts")
    parser.add_argument("db", nargs="?", default="/tmp/accounts.db")
    parser.add_argument("--base-url", default="http://127.0.0.1:8008")
    parser.add_argument("--auth", default="gork", help="admin bearer token (app_key)")
    parser.add_argument("--batch-size", type=int, default=100)
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument(
        "--disable-sso-soft-fail",
        action="store_true",
        help="disable active accounts with SSO fail and no last_ok_at",
    )
    parser.add_argument(
        "--delete-never-used-failed",
        action="store_true",
        help="delete active invalid_credentials never-used accounts",
    )
    parser.add_argument(
        "--skip-expired",
        action="store_true",
        help="do not delete status=expired",
    )
    args = parser.parse_args()

    path = Path(args.db)
    if not path.exists():
        print(f"db not found: {path}", file=sys.stderr)
        print("hint: docker cp gork:/app/data/accounts.db /tmp/accounts.db", file=sys.stderr)
        return 1

    conn = sqlite3.connect(f"file:{path}?mode=ro", uri=True)
    expired = [] if args.skip_expired else load_expired(conn)
    soft = load_sso_soft_fail_never_ok(conn) if args.disable_sso_soft_fail else []
    invalid = load_never_used_invalid(conn) if args.delete_never_used_failed else []
    conn.close()

    print(f"plan: expired_delete={len(expired)} sso_soft_disable={len(soft)} never_used_invalid_delete={len(invalid)}")
    if args.dry_run:
        print("dry-run only, no changes")
        return 0

    n1 = delete_tokens(args.base_url.rstrip("/"), args.auth, expired, args.batch_size, False)
    n2 = delete_tokens(args.base_url.rstrip("/"), args.auth, invalid, args.batch_size, False)
    n3 = disable_tokens(args.base_url.rstrip("/"), args.auth, soft, args.batch_size, False)
    print(f"done: deleted_expired={n1} deleted_invalid={n2} disabled_soft={n3}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
