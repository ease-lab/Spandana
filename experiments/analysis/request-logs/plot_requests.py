#!/usr/bin/env python3
"""Plot per-second VM and serverless request counts."""

import argparse
import csv
from pathlib import Path

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()

    buckets = []
    vm_requests = []
    serverless_requests = []
    with args.input.open(encoding="utf-8", newline="") as stream:
        for row in csv.DictReader(stream):
            buckets.append(int(row["bucket_index"]))
            vm_requests.append(int(row["total_vm_requests"]))
            serverless_requests.append(int(row["total_serverless_requests"]))

    if not buckets:
        raise SystemExit(f"no statistics rows found in {args.input}")

    args.output.parent.mkdir(parents=True, exist_ok=True)
    figure, axis = plt.subplots(figsize=(12, 6))
    axis.plot(buckets, vm_requests, label="VM requests", color="tab:orange")
    axis.plot(
        buckets,
        serverless_requests,
        label="Serverless requests",
        color="tab:purple",
    )
    axis.set_xlabel("Time since first request (seconds)")
    axis.set_ylabel("Requests per second")
    axis.set_title("VM and serverless requests")
    axis.legend()
    axis.grid(True)
    figure.tight_layout()
    figure.savefig(args.output, dpi=300)
    plt.close(figure)
    print(f"Wrote {args.output}")


if __name__ == "__main__":
    main()
