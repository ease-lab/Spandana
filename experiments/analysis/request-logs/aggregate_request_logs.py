#!/usr/bin/env python3
"""Aggregate SLO Director request logs into relative one-second buckets."""

import argparse
import csv
import json
import re
from collections import Counter, defaultdict
from datetime import datetime, timedelta, timezone
from pathlib import Path

LINE_PATTERN = re.compile(
    r"^\[(?P<timestamp>[^]]+)]\s+"
    r"(?P<backend>.+?)\s+-\s+"
    r"Request Duration:\s*(?P<request_duration>[^,]+),\s*"
    r"Total Duration:\s*(?P<total_duration>[^,]+),\s*"
    r"Status Code:\s*(?P<status_code>\d+)\s*$"
)
DURATION_COMPONENT = re.compile(r"(\d+(?:\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h)")
UNIT_TO_MILLISECONDS = {
    "ns": 1e-6,
    "us": 1e-3,
    "µs": 1e-3,
    "μs": 1e-3,
    "ms": 1.0,
    "s": 1_000.0,
    "m": 60_000.0,
    "h": 3_600_000.0,
}


def parse_go_duration_ms(value: str) -> float:
    """Parse Go time.Duration.String output and return milliseconds."""
    text = value.strip()
    sign = -1.0 if text.startswith("-") else 1.0
    if text[:1] in {"-", "+"}:
        text = text[1:]

    position = 0
    total_ms = 0.0
    for match in DURATION_COMPONENT.finditer(text):
        if match.start() != position:
            raise ValueError(f"invalid Go duration: {value!r}")
        total_ms += float(match.group(1)) * UNIT_TO_MILLISECONDS[match.group(2)]
        position = match.end()

    if position != len(text) or position == 0:
        raise ValueError(f"invalid Go duration: {value!r}")
    return sign * total_ms


def parse_timestamp(value: str) -> datetime:
    normalized = value[:-1] + "+00:00" if value.endswith("Z") else value
    timestamp = datetime.fromisoformat(normalized)
    if timestamp.tzinfo is None:
        raise ValueError(f"timestamp has no timezone: {value!r}")
    return timestamp.astimezone(timezone.utc)


def format_timestamp(timestamp: datetime) -> str:
    return timestamp.astimezone(timezone.utc).isoformat(
        timespec="microseconds"
    ).replace("+00:00", "Z")


def normalize_backend(value: str) -> str:
    normalized = value.strip().lower()
    if normalized.startswith("vm"):
        return "VM Backend"
    if "serverless" in normalized:
        return "Serverless Backend"
    raise ValueError(f"unknown backend: {value!r}")


def parse_log_line(line: str) -> dict:
    match = LINE_PATTERN.fullmatch(line.strip())
    if not match:
        raise ValueError("line does not match the SLO Director log format")
    return {
        "timestamp": parse_timestamp(match.group("timestamp")),
        "backend": normalize_backend(match.group("backend")),
        "request_duration_ms": parse_go_duration_ms(match.group("request_duration")),
        "total_duration_ms": parse_go_duration_ms(match.group("total_duration")),
        "status_code": int(match.group("status_code")),
    }


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input-dir", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument(
        "--latency-field",
        choices=("request", "total"),
        default="request",
        help="Duration recorded in latency distributions (default: request).",
    )
    args = parser.parse_args()

    log_files = sorted(args.input_dir.rglob("request_logs_*.txt"))
    if not log_files:
        parser.error(f"no request_logs_*.txt files found under {args.input_dir}")

    records = []
    errors = []
    backend_counts = Counter()
    status_counts = Counter()

    for log_file in log_files:
        source = str(log_file.relative_to(args.input_dir))
        with log_file.open(encoding="utf-8") as stream:
            for line_number, line in enumerate(stream, start=1):
                if not line.strip():
                    continue
                try:
                    record = parse_log_line(line)
                except ValueError as error:
                    errors.append(f"{log_file}:{line_number}: {error}")
                    continue
                record["source"] = source
                records.append(record)
                backend_counts[record["backend"]] += 1
                status_counts[record["status_code"]] += 1

    if errors:
        details = "\n".join(errors[:10])
        remainder = len(errors) - min(len(errors), 10)
        if remainder:
            details += f"\n... and {remainder} more malformed lines"
        raise SystemExit(f"refusing to omit {len(errors)} malformed log lines:\n{details}")
    if not records:
        raise SystemExit("no request records were found")

    earliest = min(record["timestamp"] for record in records)
    buckets = defaultdict(lambda: defaultdict(list))
    contributors = defaultdict(set)
    latency_key = f"{args.latency_field}_duration_ms"

    for record in records:
        bucket = int((record["timestamp"] - earliest).total_seconds())
        buckets[bucket][record["backend"]].append(record[latency_key])
        contributors[bucket].add(record["source"])

    args.output.parent.mkdir(parents=True, exist_ok=True)
    with args.output.open("w", encoding="utf-8", newline="") as stream:
        writer = csv.writer(stream)
        writer.writerow(
            (
                "bucket_index",
                "range_start",
                "range_end",
                "backend",
                "latencies_ms",
                "active_proxies",
            )
        )
        backend_order = ("VM Backend", "Serverless Backend")
        for bucket in sorted(buckets):
            range_start = earliest + timedelta(seconds=bucket)
            range_end = range_start + timedelta(seconds=1)
            for backend in backend_order:
                latencies = buckets[bucket].get(backend)
                if not latencies:
                    continue
                writer.writerow(
                    (
                        bucket,
                        format_timestamp(range_start),
                        format_timestamp(range_end),
                        backend,
                        json.dumps(latencies, separators=(",", ":")),
                        len(contributors[bucket]),
                    )
                )

    print(f"Parsed {len(records)} requests from {len(log_files)} log files.")
    print(f"Backend counts: {dict(backend_counts)}")
    print(f"Status counts: {dict(status_counts)}")
    print(f"Wrote {args.output}")


if __name__ == "__main__":
    main()
