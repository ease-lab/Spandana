#!/usr/bin/env python3
"""Validate and count requests in SLO Director log files."""

import argparse
from collections import Counter
from pathlib import Path

from aggregate_request_logs import parse_log_line


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input-dir", type=Path, required=True)
    args = parser.parse_args()

    log_files = sorted(args.input_dir.rglob("request_logs_*.txt"))
    if not log_files:
        parser.error(f"no request_logs_*.txt files found under {args.input_dir}")

    total = 0
    backend_counts = Counter()
    for log_file in log_files:
        file_total = 0
        with log_file.open(encoding="utf-8") as stream:
            for line_number, line in enumerate(stream, start=1):
                if not line.strip():
                    continue
                try:
                    record = parse_log_line(line)
                except ValueError as error:
                    raise SystemExit(f"{log_file}:{line_number}: {error}") from error
                backend_counts[record["backend"]] += 1
                file_total += 1
        total += file_total
        print(f"{log_file.name}: {file_total}")

    for backend in ("VM Backend", "Serverless Backend"):
        print(f"{backend}: {backend_counts[backend]}")
    print(f"Total requests: {total}")


if __name__ == "__main__":
    main()
