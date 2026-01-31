#!/usr/bin/env python3
"""
Compute notification gap statistics from gateway logs.

Usage:
  # drop logs into analysis/data/
  python analysis/notify_gap_stats.py              # uses newest file in analysis/data/
  python analysis/notify_gap_stats.py analysis/data/logs.txt
  cat logfile | python analysis/notify_gap_stats.py

The script scans for lines that contain "notify payload", extracts the leading
timestamp, and reports gap statistics in seconds between successive notifications.
"""

import argparse
import sys
from datetime import datetime
from pathlib import Path
from typing import Iterable, List, Optional


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "logfile", nargs="?", help="Gateway log file (defaults to stdin if omitted)"
    )
    return parser.parse_args()


def default_logfile() -> Optional[Path]:
    # Look for the newest file in analysis/data to make manual drops easy.
    data_dir = Path(__file__).resolve().parent / "data"
    if not data_dir.is_dir():
        return None
    files = [p for p in data_dir.iterdir() if p.is_file()]
    if not files:
        return None
    return max(files, key=lambda p: p.stat().st_mtime)


def extract_timestamps(lines: Iterable[str]) -> List[datetime]:
    timestamps: List[datetime] = []
    for line in lines:
        if "notify payload" not in line:
            continue
        # Timestamp is the first two whitespace-separated tokens.
        parts = line.split(maxsplit=2)
        if len(parts) < 2:
            continue
        dt_str = f"{parts[0]} {parts[1]}"
        try:
            timestamps.append(datetime.strptime(dt_str, "%Y/%m/%d %H:%M:%S.%f"))
        except ValueError:
            continue
    return timestamps


def percentile(sorted_values: List[float], p: float) -> Optional[float]:
    if not sorted_values:
        return None
    k = (len(sorted_values) - 1) * p
    f = int(k)
    c = min(f + 1, len(sorted_values) - 1)
    if f == c:
        return sorted_values[f]
    return sorted_values[f] + (sorted_values[c] - sorted_values[f]) * (k - f)


def compute_gaps(timestamps: List[datetime]) -> List[float]:
    return [
        (timestamps[i] - timestamps[i - 1]).total_seconds()
        for i in range(1, len(timestamps))
    ]


def format_float(value: Optional[float]) -> str:
    return f"{value:.6f}" if value is not None else "-"


def main() -> int:
    args = parse_args()
    logfile = Path(args.logfile) if args.logfile else default_logfile()
    if logfile:
        with open(logfile, "r", encoding="utf-8") as f:
            lines = f.readlines()
        print(f"using logfile: {logfile}")
    else:
        lines = sys.stdin.readlines()

    timestamps = extract_timestamps(lines)
    if len(timestamps) < 2:
        print("Not enough notifications found to compute gaps.")
        return 1

    gaps = compute_gaps(timestamps)
    gaps_sorted = sorted(gaps)

    print(f"notifications: {len(timestamps)}")
    print(f"gaps count  : {len(gaps)}")
    print(f"gap min     : {format_float(min(gaps))} s")
    print(f"gap max     : {format_float(max(gaps))} s")
    print(f"gap p50     : {format_float(percentile(gaps_sorted, 0.50))} s")
    print(f"gap p90     : {format_float(percentile(gaps_sorted, 0.90))} s")
    print(f"gap p95     : {format_float(percentile(gaps_sorted, 0.95))} s")
    print(f"gap p99     : {format_float(percentile(gaps_sorted, 0.99))} s")
    print(f"mean gap    : {format_float(sum(gaps) / len(gaps))} s")

    # Count noticeable stalls: >1s and >3s
    over_1s = sum(1 for g in gaps if g > 1.0)
    over_3s = sum(1 for g in gaps if g > 3.0)
    print(f"gaps >1s    : {over_1s}")
    print(f"gaps >3s    : {over_3s}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
