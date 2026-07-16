#!/usr/bin/env python3
"""Summarize gork accounts.db health for 403/429 operations.

Usage:
  python3 scripts/account_health.py [path/to/accounts.db]
  python3 scripts/account_health.py data/accounts.db --json

If the host cannot read data/accounts.db (permissions), copy from container:
  docker cp gork:/app/data/accounts.db /tmp/accounts.db
  python3 scripts/account_health.py /tmp/accounts.db
"""

from __future__ import annotations

import argparse
import json
import sqlite3
import sys
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path


def load_json(value: str | None) -> dict:
    if not value:
        return {}
    try:
        data = json.loads(value)
        return data if isinstance(data, dict) else {}
    except json.JSONDecodeError:
        return {}


def ms_to_day(ms: int | None) -> str:
    if not ms:
        return "unknown"
    try:
        return datetime.fromtimestamp(ms / 1000.0, tz=timezone.utc).strftime("%Y-%m-%d")
    except (OverflowError, OSError, ValueError):
        return "invalid"


def analyze_db(path: Path) -> dict:
    conn = sqlite3.connect(f"file:{path}?mode=ro", uri=True)
    conn.row_factory = sqlite3.Row
    cur = conn.cursor()

    total = cur.execute("SELECT COUNT(*) AS c FROM accounts").fetchone()["c"]
    status_dist = dict(
        cur.execute(
            "SELECT COALESCE(status,'null'), COUNT(*) FROM accounts GROUP BY 1 ORDER BY 2 DESC"
        ).fetchall()
    )
    pool_dist = dict(
        cur.execute(
            "SELECT COALESCE(pool,'null'), COUNT(*) FROM accounts GROUP BY 1 ORDER BY 2 DESC"
        ).fetchall()
    )
    fail_reason = dict(
        cur.execute(
            "SELECT COALESCE(last_fail_reason,'(none)'), COUNT(*) FROM accounts "
            "GROUP BY 1 ORDER BY 2 DESC LIMIT 20"
        ).fetchall()
    )
    state_reason = dict(
        cur.execute(
            "SELECT COALESCE(state_reason,'(none)'), COUNT(*) FROM accounts "
            "GROUP BY 1 ORDER BY 2 DESC LIMIT 20"
        ).fetchall()
    )

    usage = cur.execute(
        """
        SELECT
          COALESCE(SUM(usage_use_count), 0),
          COALESCE(SUM(usage_fail_count), 0),
          SUM(CASE WHEN usage_use_count > 0 THEN 1 ELSE 0 END),
          SUM(CASE WHEN usage_use_count IS NULL OR usage_use_count = 0 THEN 1 ELSE 0 END),
          SUM(CASE WHEN usage_use_count > 0 AND COALESCE(usage_fail_count,0) = 0 THEN 1 ELSE 0 END),
          SUM(CASE WHEN status = 'active' AND last_fail_reason = 'rate_limited' THEN 1 ELSE 0 END),
          SUM(CASE WHEN status = 'active' AND last_fail_reason = 'invalid_credentials' THEN 1 ELSE 0 END),
          SUM(CASE WHEN deleted_at IS NOT NULL THEN 1 ELSE 0 END)
        FROM accounts
        """
    ).fetchone()

    created_days = dict(
        cur.execute(
            """
            SELECT date(created_at/1000, 'unixepoch'), COUNT(*)
            FROM accounts
            GROUP BY 1
            ORDER BY 1 DESC
            LIMIT 14
            """
        ).fetchall()
    )
    updated_days = dict(
        cur.execute(
            """
            SELECT date(updated_at/1000, 'unixepoch'), COUNT(*)
            FROM accounts
            GROUP BY 1
            ORDER BY 1 DESC
            LIMIT 14
            """
        ).fetchall()
    )

    # Sample active quota_console distribution
    remaining = Counter()
    zero_quota = 0
    null_quota = 0
    positive_quota = 0
    blocked_markers = 0
    rows = cur.execute(
        "SELECT status, quota_console, ext, last_fail_reason, usage_use_count, usage_fail_count "
        "FROM accounts WHERE status = 'active'"
    )
    active_count = 0
    for row in rows:
        active_count += 1
        qc = load_json(row["quota_console"])
        ext = load_json(row["ext"])
        rem = qc.get("remaining")
        if rem is None:
            null_quota += 1
        elif int(rem) <= 0:
            zero_quota += 1
            remaining[0] += 1
        else:
            positive_quota += 1
            remaining[int(rem)] += 1
        ic = ext.get("invalid_credentials")
        if isinstance(ic, dict):
            err = str(ic.get("last_fail_error", "")).lower()
            if "blocked" in err:
                blocked_markers += 1

    expired_invalid = cur.execute(
        "SELECT COUNT(*) FROM accounts WHERE status = 'expired' AND "
        "(last_fail_reason = 'invalid_credentials' OR state_reason = 'invalid_credentials')"
    ).fetchone()[0]

    never_used_active = cur.execute(
        "SELECT COUNT(*) FROM accounts WHERE status = 'active' AND "
        "(usage_use_count IS NULL OR usage_use_count = 0)"
    ).fetchone()[0]

    conn.close()

    used = int(usage[2] or 0)
    never_used = int(usage[3] or 0)
    success_only = int(usage[4] or 0)

    report = {
        "db": str(path),
        "total_accounts": total,
        "status": status_dist,
        "pools": pool_dist,
        "last_fail_reason": fail_reason,
        "state_reason": state_reason,
        "usage": {
            "sum_use_count": int(usage[0] or 0),
            "sum_fail_count": int(usage[1] or 0),
            "accounts_used": used,
            "accounts_never_used": never_used,
            "accounts_used_without_fail": success_only,
            "active_rate_limited": int(usage[5] or 0),
            "active_invalid_credentials": int(usage[6] or 0),
            "deleted_at_set": int(usage[7] or 0),
        },
        "active": {
            "count": active_count,
            "never_used": never_used_active,
            "quota_console_positive": positive_quota,
            "quota_console_zero": zero_quota,
            "quota_console_null": null_quota,
            "remaining_top": dict(remaining.most_common(10)),
            "ext_blocked_markers": blocked_markers,
        },
        "expired_invalid_credentials": expired_invalid,
        "created_by_day": created_days,
        "updated_by_day": updated_days,
        "health_score": {},
        "recommendations": [],
    }

    # Simple health score 0-100
    if total <= 0:
        score = 0.0
    else:
        used_ratio = used / total
        success_ratio = success_only / max(used, 1)
        expired_ratio = status_dist.get("expired", 0) / total
        score = min(
            100.0,
            40.0 * min(used_ratio * 20, 1.0)  # reward validated usage (cap early)
            + 40.0 * success_ratio
            + 20.0 * (1.0 - min(expired_ratio * 5, 1.0)),
        )
    report["health_score"] = {
        "score": round(score, 1),
        "used_ratio_pct": round(100.0 * used / total, 3) if total else 0.0,
        "success_among_used_pct": round(100.0 * success_only / used, 1) if used else 0.0,
        "expired_pct": round(100.0 * status_dist.get("expired", 0) / total, 2) if total else 0.0,
    }

    recs = report["recommendations"]
    if used / max(total, 1) < 0.01:
        recs.append(
            f"号池几乎未验证：{used}/{total} 用过。导入后应抽样 console 预检，避免运行时才发现 blocked-user。"
        )
    if expired_invalid > 0:
        recs.append(
            f"已有 {expired_invalid} 个 invalid_credentials/expired。持续 403 blocked 时优先停用该批次来源。"
        )
    if report["usage"]["active_rate_limited"] > 0:
        recs.append(
            f"{report['usage']['active_rate_limited']} 个 active 账号最近 rate_limited：降低 max_inflight / multi-agent QPS。"
        )
    if never_used_active > 10000:
        recs.append(
            f"active 未使用账号 {never_used_active} 过多，体积拖累选号与存储；考虑分批导入与淘汰。"
        )
    if success_only < 50 and total > 1000:
        recs.append(
            "真正无失败使用过的账号很少：当前 403 更可能是号质问题，而不是单纯并发配置。"
        )

    return report


def print_human(report: dict) -> None:
    print("=== Gork 号池健康度 ===")
    print(f"db: {report['db']}")
    print(f"total: {report['total_accounts']}")
    print(f"status: {report['status']}")
    print(f"pools: {report['pools']}")
    hs = report["health_score"]
    print(
        f"health_score: {hs['score']}/100  "
        f"used={hs['used_ratio_pct']}%  "
        f"success_among_used={hs['success_among_used_pct']}%  "
        f"expired={hs['expired_pct']}%"
    )
    print()
    print("--- Usage ---")
    for k, v in report["usage"].items():
        print(f"  {k}: {v}")
    print()
    print("--- Active quota_console ---")
    for k, v in report["active"].items():
        print(f"  {k}: {v}")
    print()
    print("--- Fail reasons ---")
    for k, v in list(report["last_fail_reason"].items())[:10]:
        print(f"  {v:6d}  {k}")
    print()
    print("--- Created (recent days) ---")
    for k, v in list(report["created_by_day"].items())[:8]:
        print(f"  {k}: {v}")
    print()
    print("--- Recommendations ---")
    if not report["recommendations"]:
        print("  (none)")
    for rec in report["recommendations"]:
        print(f"  - {rec}")


def main() -> int:
    parser = argparse.ArgumentParser(description="Summarize gork accounts.db health")
    parser.add_argument(
        "db",
        nargs="?",
        default="data/accounts.db",
        help="path to accounts.db (default: data/accounts.db)",
    )
    parser.add_argument("--json", action="store_true", help="emit JSON report")
    args = parser.parse_args()

    path = Path(args.db)
    if not path.exists():
        print(f"db not found: {path}", file=sys.stderr)
        print(
            "hint: docker cp gork:/app/data/accounts.db /tmp/accounts.db",
            file=sys.stderr,
        )
        return 1

    try:
        report = analyze_db(path)
    except sqlite3.Error as exc:
        print(f"sqlite error: {exc}", file=sys.stderr)
        return 1

    if args.json:
        json.dump(report, sys.stdout, ensure_ascii=False, indent=2)
        print()
    else:
        print_human(report)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
