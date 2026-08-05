#!/usr/bin/env python3
"""Generate per-second request counts and rolling latency statistics."""

import argparse
import csv
import json
import math
from collections import defaultdict
from datetime import timedelta
from pathlib import Path

from aggregate_request_logs import format_timestamp, parse_timestamp


def percentile(values: list[float], percent: float) -> float | None:
    if not values:
        return None
    ordered = sorted(values)
    position = (len(ordered) - 1) * percent
    lower = math.floor(position)
    upper = math.ceil(position)
    if lower == upper:
        return ordered[lower]
    fraction = position - lower
    return ordered[lower] * (1 - fraction) + ordered[upper] * fraction


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--window-seconds", type=int, default=1)
    args = parser.parse_args()
    if args.window_seconds < 1:
        parser.error("--window-seconds must be at least 1")

    latencies = defaultdict(lambda: defaultdict(list))
    active_proxies = defaultdict(int)
    origin = None
    maximum_bucket = -1

    with args.input.open(encoding="utf-8", newline="") as stream:
        reader = csv.DictReader(stream)
        for row in reader:
            bucket = int(row["bucket_index"])
            range_start = parse_timestamp(row["range_start"])
            candidate_origin = range_start - timedelta(seconds=bucket)
            if origin is None:
                origin = candidate_origin
            elif candidate_origin != origin:
                raise SystemExit("aggregate contains inconsistent bucket timestamps")
            latencies[bucket][row["backend"]].extend(json.loads(row["latencies_ms"]))
            active_proxies[bucket] = max(
                active_proxies[bucket], int(row["active_proxies"])
            )
            maximum_bucket = max(maximum_bucket, bucket)

    if origin is None:
        raise SystemExit(f"no aggregate rows found in {args.input}")

    args.output.parent.mkdir(parents=True, exist_ok=True)
    with args.output.open("w", encoding="utf-8", newline="") as stream:
        fieldnames = (
            "bucket_index",
            "range_end",
            "vm_median_latency_ms",
            "vm_p95_latency_ms",
            "serverless_median_latency_ms",
            "serverless_p95_latency_ms",
            "total_vm_requests",
            "total_serverless_requests",
            "total_requests",
            "active_proxies",
        )
        writer = csv.DictWriter(stream, fieldnames=fieldnames)
        writer.writeheader()

        for bucket in range(maximum_bucket + 1):
            window_start = max(0, bucket - args.window_seconds + 1)
            vm_window = []
            serverless_window = []
            for window_bucket in range(window_start, bucket + 1):
                vm_window.extend(latencies[window_bucket].get("VM Backend", ()))
                serverless_window.extend(
                    latencies[window_bucket].get("Serverless Backend", ())
                )

            vm_requests = len(latencies[bucket].get("VM Backend", ()))
            serverless_requests = len(
                latencies[bucket].get("Serverless Backend", ())
            )
            writer.writerow(
                {
                    "bucket_index": bucket,
                    "range_end": format_timestamp(
                        origin + timedelta(seconds=bucket + 1)
                    ),
                    "vm_median_latency_ms": percentile(vm_window, 0.5),
                    "vm_p95_latency_ms": percentile(vm_window, 0.95),
                    "serverless_median_latency_ms": percentile(
                        serverless_window, 0.5
                    ),
                    "serverless_p95_latency_ms": percentile(
                        serverless_window, 0.95
                    ),
                    "total_vm_requests": vm_requests,
                    "total_serverless_requests": serverless_requests,
                    "total_requests": vm_requests + serverless_requests,
                    "active_proxies": active_proxies[bucket],
                }
            )

    print(
        f"Wrote {maximum_bucket + 1} one-second buckets to {args.output} "
        f"using a {args.window_seconds}-second latency window."
    )


if __name__ == "__main__":
    main()
