#!/usr/bin/env python3
"""Classify 403/429 from gork access + upstream logs.

Usage:
  python3 scripts/analyze_403_429.py [log_dir_or_file ...]
  python3 scripts/analyze_403_429.py logs/
  python3 scripts/analyze_403_429.py logs/app_2026-07-08.log --json

Default log dir: ./logs
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from collections import Counter, defaultdict
from pathlib import Path

KV_RE = re.compile(r'(\w+)=("(?:\\.|[^"])*"|[^\s]+)')


def parse_line(line: str) -> dict[str, str]:
    out: dict[str, str] = {}
    for key, value in KV_RE.findall(line):
        if value.startswith('"') and value.endswith('"'):
            value = value[1:-1]
        out[key] = value
    return out


def collect_files(paths: list[str]) -> list[Path]:
    files: list[Path] = []
    for raw in paths:
        path = Path(raw)
        if path.is_file():
            files.append(path)
        elif path.is_dir():
            files.extend(sorted(path.glob("app_*.log")))
            files.extend(sorted(path.glob("*.log")))
        else:
            print(f"warn: skip missing path {path}", file=sys.stderr)
    # de-dupe preserve order
    seen: set[Path] = set()
    unique: list[Path] = []
    for f in files:
        rp = f.resolve()
        if rp not in seen:
            seen.add(rp)
            unique.append(f)
    return unique


def classify_403_body(body: str) -> str:
    text = body.lower()
    if "blocked-user" in text or "user is blocked" in text:
        return "blocked_user"
    if "invalid-credentials" in text or "bad-credentials" in text:
        return "invalid_credentials"
    if "token expired" in text or "token revoked" in text:
        return "token_expired"
    if "cloudflare" in text or "cf-challenge" in text or "just a moment" in text:
        return "cloudflare_challenge"
    if "email-domain-rejected" in text:
        return "email_domain_rejected"
    if body.strip():
        return "other_403"
    return "empty_body"


def classify_429_body(body: str) -> dict[str, str | int]:
    info: dict[str, str | int] = {"kind": "other_429"}
    if "resource-exhausted" in body or "too many requests" in body.lower():
        info["kind"] = "upstream_team_rate_limit"
    model = re.search(r"model ([^\s.]+(?:\.[^\s]+)*)", body)
    if model:
        info["model"] = model.group(1).rstrip(".")
    rpm = re.search(r"Requests per Minute \(actual/limit\): (\d+)/(\d+)", body)
    if rpm:
        info["limit_type"] = "RPM"
        info["actual"] = int(rpm.group(1))
        info["limit"] = int(rpm.group(2))
    rps = re.search(r"Requests per Second \(actual/limit\): (\d+)/(\d+)", body)
    if rps:
        info["limit_type"] = "RPS"
        info["actual"] = int(rps.group(1))
        info["limit"] = int(rps.group(2))
    return info


def duration_bucket(ms: int, for_status: str) -> str:
    if for_status == "429":
        if ms <= 2:
            return "local_fast(<=2ms)"
        if ms < 100:
            return "1-99ms"
        if ms < 1000:
            return "100-999ms"
        if ms < 5000:
            return "1-5s"
        if ms < 30000:
            return "5-30s"
        if ms < 120000:
            return "30-120s"
        return ">120s"
    if ms < 1000:
        return "<1s"
    if ms < 5000:
        return "1-5s"
    if ms < 30000:
        return "5-30s"
    if ms < 120000:
        return "30-120s"
    return ">120s"


def analyze(files: list[Path]) -> dict:
    chat_status: Counter[str] = Counter()
    daily_chat: dict[str, Counter[str]] = defaultdict(Counter)
    http_403_path: Counter[str] = Counter()
    http_429_path: Counter[str] = Counter()
    upstream_403_class: Counter[str] = Counter()
    upstream_429_class: Counter[str] = Counter()
    upstream_429_model: Counter[str] = Counter()
    upstream_429_limit_type: Counter[str] = Counter()
    dur_403: Counter[str] = Counter()
    dur_429: Counter[str] = Counter()
    account_events: Counter[str] = Counter()
    hourly_http: Counter[tuple[str, str]] = Counter()
    daily_upstream: dict[str, Counter[str]] = defaultdict(Counter)
    daily_deleted: Counter[str] = Counter()
    rpm_actuals: list[int] = []
    rps_actuals: list[int] = []
    client_403 = client_429 = up_403 = up_429 = 0

    for path in files:
        with path.open(errors="replace") as fh:
            for line in fh:
                d = parse_line(line)
                msg = d.get("msg", "")
                status = d.get("status", "")
                path_name = d.get("path", "")
                time_s = d.get("time", "")
                day = time_s[:10] if time_s else path.stem
                hour = time_s[11:13] if len(time_s) >= 13 else "?"

                if msg == "http access":
                    if path_name == "/v1/chat/completions":
                        chat_status[status] += 1
                        daily_chat[day][status] += 1
                    if status in ("403", "429"):
                        try:
                            dur = int(d.get("duration_ms", "0") or "0")
                        except ValueError:
                            dur = 0
                        if status == "403":
                            http_403_path[path_name] += 1
                            dur_403[duration_bucket(dur, "403")] += 1
                            hourly_http[(day, hour, "403")] += 1
                            client_403 += 1
                        else:
                            http_429_path[path_name] += 1
                            dur_429[duration_bucket(dur, "429")] += 1
                            hourly_http[(day, hour, "429")] += 1
                            client_429 += 1
                    continue

                if "console upstream request failed" in msg or msg.endswith("upstream request failed"):
                    body = d.get("body_excerpt", "")
                    if status == "403":
                        up_403 += 1
                        daily_upstream[day]["403"] += 1
                        upstream_403_class[classify_403_body(body)] += 1
                    elif status == "429":
                        up_429 += 1
                        daily_upstream[day]["429"] += 1
                        info = classify_429_body(body)
                        upstream_429_class[str(info["kind"])] += 1
                        if "model" in info:
                            upstream_429_model[str(info["model"])] += 1
                        if "limit_type" in info:
                            upstream_429_limit_type[str(info["limit_type"])] += 1
                        if info.get("limit_type") == "RPM" and isinstance(info.get("actual"), int):
                            rpm_actuals.append(int(info["actual"]))
                        if info.get("limit_type") == "RPS" and isinstance(info.get("actual"), int):
                            rps_actuals.append(int(info["actual"]))
                    continue

                if "invalid credentials" in msg.lower() or "account deleted" in msg.lower() or "account expired" in msg.lower():
                    account_events[msg] += 1
                    if "deleted" in msg:
                        daily_deleted[day] += 1

    total_chat = sum(chat_status.values())
    local_fast_429 = dur_429.get("local_fast(<=2ms)", 0)

    report = {
        "files": [str(f) for f in files],
        "chat_completions": {
            "total": total_chat,
            "by_status": dict(chat_status.most_common()),
            "success_rate_pct": round(100.0 * chat_status.get("200", 0) / total_chat, 2) if total_chat else 0.0,
        },
        "client": {
            "403": client_403,
            "429": client_429,
            "403_by_path": dict(http_403_path.most_common()),
            "429_by_path": dict(http_429_path.most_common()),
            "403_duration_buckets": dict(dur_403.most_common()),
            "429_duration_buckets": dict(dur_429.most_common()),
            "local_fast_429": local_fast_429,
            "local_fast_429_share_pct": round(100.0 * local_fast_429 / client_429, 2) if client_429 else 0.0,
        },
        "upstream": {
            "403": up_403,
            "429": up_429,
            "403_classes": dict(upstream_403_class.most_common()),
            "429_classes": dict(upstream_429_class.most_common()),
            "429_models": dict(upstream_429_model.most_common()),
            "429_limit_types": dict(upstream_429_limit_type.most_common()),
            "rpm_actual_max": max(rpm_actuals) if rpm_actuals else None,
            "rpm_actual_avg": round(sum(rpm_actuals) / len(rpm_actuals), 1) if rpm_actuals else None,
            "rps_actual_max": max(rps_actuals) if rps_actuals else None,
            "amplification": {
                "403_up_per_client": round(up_403 / client_403, 2) if client_403 else None,
                "429_up_per_client": round(up_429 / client_429, 2) if client_429 else None,
            },
        },
        "account_events": dict(account_events.most_common()),
        "daily": [],
        "peak_hours": {
            "429": [],
            "403": [],
        },
        "recommendations": [],
    }

    for day in sorted(set(list(daily_chat) + list(daily_upstream) + list(daily_deleted))):
        c = daily_chat.get(day, Counter())
        total = sum(c.values())
        report["daily"].append(
            {
                "day": day,
                "chat_total": total,
                "chat_200": c.get("200", 0),
                "chat_403": c.get("403", 0),
                "chat_429": c.get("429", 0),
                "chat_502": c.get("502", 0),
                "success_rate_pct": round(100.0 * c.get("200", 0) / total, 1) if total else 0.0,
                "upstream_403": daily_upstream.get(day, Counter()).get("403", 0),
                "upstream_429": daily_upstream.get(day, Counter()).get("429", 0),
                "accounts_deleted": daily_deleted.get(day, 0),
            }
        )

    peaks_429 = sorted(
        ((f"{d} {h}", n) for (d, h, s), n in hourly_http.items() if s == "429"),
        key=lambda x: x[1],
        reverse=True,
    )[:10]
    peaks_403 = sorted(
        ((f"{d} {h}", n) for (d, h, s), n in hourly_http.items() if s == "403"),
        key=lambda x: x[1],
        reverse=True,
    )[:10]
    report["peak_hours"]["429"] = [{"hour": h, "count": n} for h, n in peaks_429]
    report["peak_hours"]["403"] = [{"hour": h, "count": n} for h, n in peaks_403]

    # Actionable recommendations from the numbers
    recs = report["recommendations"]
    if upstream_403_class.get("blocked_user", 0) > 0:
        share = 100.0 * upstream_403_class["blocked_user"] / max(up_403, 1)
        recs.append(
            f"403 主因是 blocked_user（{upstream_403_class['blocked_user']} / {up_403}, {share:.0f}%）：清洗号池并预检，不要对 blocked 换号重试。"
        )
    if upstream_403_class.get("cloudflare_challenge", 0) > 0:
        recs.append("出现 Cloudflare challenge 403：检查 clearance / WARP / 出口 IP。")
    if upstream_429_model:
        top_model, top_n = upstream_429_model.most_common(1)[0]
        recs.append(
            f"429 集中在模型 {top_model}（{top_n} 次）：对该模型单独降并发 / 加长 cooldown。"
        )
    if upstream_429_limit_type.get("RPS", 0) and upstream_429_limit_type.get("RPM", 0):
        recs.append(
            f"同时撞到 RPS({upstream_429_limit_type['RPS']}) 与 RPM({upstream_429_limit_type['RPM']})：max_inflight 建议 1–2，并保留全局 min_interval。"
        )
    if client_429 and local_fast_429 / client_429 > 0.2:
        recs.append(
            f"本地快失败 429 占比 {100.0 * local_fast_429 / client_429:.0f}%：pacer/cooldown 在挡流量；可提高 max_queue_wait 换延迟降错误，或从客户端限 QPS。"
        )
    if total_chat and chat_status.get("200", 0) / total_chat < 0.3:
        recs.append(
            f"chat 成功率仅 {report['chat_completions']['success_rate_pct']}%：优先处理号池质量与 multi-agent 限流，而不是盲目加号。"
        )

    return report


def print_human(report: dict) -> None:
    chat = report["chat_completions"]
    client = report["client"]
    up = report["upstream"]

    print("=== Gork 403/429 日志分析 ===")
    print(f"files: {len(report['files'])}")
    print(
        f"chat/completions: total={chat['total']} success={chat['success_rate_pct']}% "
        f"by_status={chat['by_status']}"
    )
    print()
    print("--- Client ---")
    print(f"403={client['403']} 429={client['429']}")
    print(f"403 paths: {client['403_by_path']}")
    print(f"429 paths: {client['429_by_path']}")
    print(f"403 duration: {client['403_duration_buckets']}")
    print(f"429 duration: {client['429_duration_buckets']}")
    print(
        f"local_fast_429: {client['local_fast_429']} ({client['local_fast_429_share_pct']}%)"
    )
    print()
    print("--- Upstream ---")
    print(f"403={up['403']} 429={up['429']} amp={up['amplification']}")
    print(f"403 classes: {up['403_classes']}")
    print(f"429 classes: {up['429_classes']}")
    print(f"429 models: {up['429_models']}")
    print(f"429 limit types: {up['429_limit_types']}")
    print(f"RPM actual avg/max: {up['rpm_actual_avg']} / {up['rpm_actual_max']}")
    print(f"RPS actual max: {up['rps_actual_max']}")
    print()
    print("--- Account events ---")
    for k, v in report["account_events"].items():
        print(f"  {v:5d}  {k}")
    print()
    print("--- Daily ---")
    print(
        f"{'day':12} {'tot':>5} {'ok%':>6} {'c403':>5} {'c429':>5} {'c502':>5} {'u403':>5} {'u429':>5} {'del':>5}"
    )
    for row in report["daily"]:
        print(
            f"{row['day']:12} {row['chat_total']:5d} {row['success_rate_pct']:5.1f}% "
            f"{row['chat_403']:5d} {row['chat_429']:5d} {row['chat_502']:5d} "
            f"{row['upstream_403']:5d} {row['upstream_429']:5d} {row['accounts_deleted']:5d}"
        )
    print()
    print("--- Peak hours ---")
    print("429:", report["peak_hours"]["429"][:5])
    print("403:", report["peak_hours"]["403"][:5])
    print()
    print("--- Recommendations ---")
    if not report["recommendations"]:
        print("  (none)")
    for rec in report["recommendations"]:
        print(f"  - {rec}")


def main() -> int:
    parser = argparse.ArgumentParser(description="Analyze gork 403/429 logs")
    parser.add_argument(
        "paths",
        nargs="*",
        default=["logs"],
        help="log files or directories (default: logs)",
    )
    parser.add_argument("--json", action="store_true", help="emit JSON report")
    args = parser.parse_args()

    files = collect_files(args.paths)
    if not files:
        print("no log files found", file=sys.stderr)
        return 1

    report = analyze(files)
    if args.json:
        json.dump(report, sys.stdout, ensure_ascii=False, indent=2)
        print()
    else:
        print_human(report)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
