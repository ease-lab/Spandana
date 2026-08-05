#!/usr/bin/env python3

import csv
import datetime
import matplotlib.pyplot as plt

def main():
    # CSV columns assumed:
    #   Timestamp (RFC3339),VMRequests,ServerlessRequests,VMMedianLatency,VMP95Latency,ServerlessMedianLatency,ServerlessP95Latency,RateLimit
    # Example header row looks like:
    # Timestamp,VMRequests,ServerlessRequests,VMMedianLatency,VMP95Latency,ServerlessMedianLatency,ServerlessP95Latency,RateLimit

    csv_file = "all_stats.csv"

    timestamps = []
    vm_requests = []
    srvless_requests = []

    # 1. Read the CSV file
    with open(csv_file, "r", newline="", encoding="utf-8") as f:
        reader = csv.reader(f)
        header = next(reader, None)  # Skip header row if present

        for row in reader:
            if not row or len(row) < 3:
                continue
            # row[0] => Timestamp (RFC3339)
            # row[1] => VMRequests
            # row[2] => ServerlessRequests
            try:
                ts = datetime.datetime.fromisoformat(row[0])
                vm = int(row[1])
                srv = int(row[2])
            except ValueError:
                # Skip any malformed rows
                continue

            timestamps.append(ts)
            vm_requests.append(vm)
            srvless_requests.append(srv)

    if not timestamps:
        print("No valid records found in CSV. Exiting.")
        return

    # 2. Prepare indices for a 'bar' style plot
    x_positions = range(len(timestamps))

    # 3. Create a figure and stacked bar chart
    plt.figure(figsize=(12, 6))

    plt.bar(x_positions, vm_requests, label="VM Requests", color="steelblue")
    plt.bar(x_positions, srvless_requests, bottom=vm_requests, label="Serverless Requests", color="darkorange")

    # 4. Format the X-axis with timestamps. For clarity, let's show only some ticks.
    #    Otherwise, if you have many data points, labels will overlap.
    plt.xticks(
        list(x_positions), 
        [ts.strftime("%H:%M:%S") for ts in timestamps],  # e.g. just show time
        rotation=45,
        ha="right"
    )
    plt.xlabel("Timestamp")
    plt.ylabel("Number of Requests")
    plt.title("VM + Serverless Requests Over Time (Stacked)")
    plt.legend()

    plt.tight_layout()
    plt.savefig('req.png')
    plt.show()

if __name__ == "__main__":
    main()
