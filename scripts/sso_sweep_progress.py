#!/usr/bin/env python3
"""Show SSO validation / bad-account cleanup progress.

Usage:
  python3 scripts/sso_sweep_progress.py [/path/to/accounts.db]
  docker cp gork:/app/data/accounts.db /tmp/accounts.db && python3 scripts/sso_sweep_progress.py /tmp/accounts.db
"""

from __future__ import annotations

import argparse
import json
import sqlite3
import sys
import time
from collections import Counter
from pathlib import Path


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("db", nargs="?", default="/tmp/accounts.db")
    parser.add_argument("--minutes", type=int, default=10, help="recent window for rate estimate")
    args = parser.parse_args()
    path = Path(args.db)
    if not path.exists():
        print(f"db not found: {path}", file=sys.stderr)
        return 1

    conn = sqlite3.connect(f"file:{path}?mode=ro", uri=True)
    c = conn.cursor()
    now = int(time.time() * 1000)
    window = args.minutes * 60 * 1000
    since = now - window

    status = dict(c.execute("SELECT status, COUNT(*) FROM accounts GROUP BY status").fetchall())
    total = sum(status.values())
    deleted = c.execute("SELECT COUNT(*) FROM accounts WHERE deleted_at IS NOT NULL").fetchone()[0]
    recent_updated = c.execute(
        "SELECT COUNT(*) FROM accounts WHERE updated_at > ?", (since,)
    ).fetchone()[0]
    recent_fails = dict(
        c.execute(
            "SELECT COALESCE(last_fail_reason,'(none)'), COUNT(*) FROM accounts "
            "WHERE updated_at > ? GROUP BY 1 ORDER BY 2 DESC",
            (since,),
        ).fetchall()
    )
    recent_deleted = c.execute(
        "SELECT COUNT(*) FROM accounts WHERE deleted_at IS NOT NULL AND deleted_at > ?",
        (since,),
    ).fetchone()[0]
    recent_expired = c.execute(
        "SELECT COUNT(*) FROM accounts WHERE status='expired' AND updated_at > ?",
        (since,),
    ).fetchone()[0]

    ok = fail = 0
    stages: Counter[str] = Counter()
    reasons: Counter[str] = Counter()
    for (ext,) in c.execute(
        "SELECT ext FROM accounts WHERE updated_at > ? AND ext IS NOT NULL", (since,)
    ):
        try:
            data = json.loads(ext)
        except json.JSONDecodeError:
            continue
        sv = data.get("sso_validation")
        if not isinstance(sv, dict):
            continue
        if int(sv.get("failure_count") or 0) == 0 and sv.get("last_ok_at"):
            ok += 1
        elif int(sv.get("failure_count") or 0) > 0:
            fail += 1
            stages[str(sv.get("last_fail_stage"))] += 1
            reasons[str(sv.get("last_fail_reason"))[:120]] += 1

    # validated ever (has last_ok_at)
    validated_ok = 0
    validated_fail = 0
    for (ext,) in c.execute("SELECT ext FROM accounts WHERE ext IS NOT NULL"):
        try:
            data = json.loads(ext)
        except json.JSONDecodeError:
            continue
        sv = data.get("sso_validation")
        if not isinstance(sv, dict):
            continue
        if sv.get("last_ok_at") and int(sv.get("failure_count") or 0) == 0:
            validated_ok += 1
        if int(sv.get("failure_count") or 0) > 0:
            validated_fail += 1

    rate_per_min = recent_updated / max(args.minutes, 1)
    remaining_active = status.get("active", 0)
    # rough: never-updated-in-this-window active still need sweep; better use never last_ok
    eta_min = remaining_active / rate_per_min if rate_per_min > 0 else None

    print("=== SSO sweep progress ===")
    print(f"db: {path}")
    print(f"total={total} status={status} deleted_at_set={deleted}")
    print(f"validated_ok(all)={validated_ok} sso_fail_markers(all)={validated_fail}")
    print(f"last_{args.minutes}m: updated={recent_updated} deleted={recent_deleted} expired_touch={recent_expired}")
    print(f"last_{args.minutes}m fail_reasons: {recent_fails}")
    print(f"last_{args.minutes}m sso ok={ok} fail={fail} stages={dict(stages)}")
    if reasons:
        print("top fail reasons:")
        for k, v in reasons.most_common(8):
            print(f"  {v:5d}  {k}")
    print(f"approx rate: {rate_per_min:.1f} accounts/min")
    if eta_min is not None:
        print(f"naive ETA for remaining active (~{remaining_active}): {eta_min/60:.1f} hours")
    print()
    print("note: only invalid-credentials (blocked-user/token expired/...) are deleted;")
    print("      'no usable quota windows' is marked fail but kept.")
    conn.close()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
