#!/usr/bin/env python3

import sys
import csv
import datetime
import matplotlib.pyplot as plt

def parse_latency_ms(lat_str):
    """
    Parse a string like '1ms' or '12.3ms' into a float (milliseconds).
    Returns 0.0 if parsing fails or string is empty.
    """
    if not lat_str:
        return 0.0
    lat_str = lat_str.strip().lower()
    if lat_str.endswith("s"):
        numeric_part = lat_str[:-1] # remove 's'
        # Convert seconds to milliseconds
        try:
            return float(numeric_part) * 1000
        except ValueError:
            return 0.0
            
    if lat_str.endswith("ms"):
        numeric_part = lat_str[:-2]  # remove 'ms'
        try:
            return float(numeric_part)
        except ValueError:
            return 0.0
    return 0.0

def main():
    # 1) Check command-line argument to pick 'median' or 'p95'
    if len(sys.argv) < 2:
        print("Usage: python plot_requests_and_latencies.py [median | p95]")
        sys.exit(1)

    mode = sys.argv[1].lower()
    if mode not in ("median", "p95"):
        print("Invalid mode. Must be 'median' or 'p95'.")
        sys.exit(1)

    # 2) Decide which CSV columns to use for VM and Serverless latencies
    #    3=VMMedianLatency, 4=VMP95Latency, 5=SvrMedianLatency, 6=SvrP95Latency
    if mode == "median":
        vm_lat_col = 3
        srv_lat_col = 5
        lat_label = "Median Latency (ms)"
        title_suffix = "Median"
    else:  # p95
        vm_lat_col = 4
        srv_lat_col = 6
        lat_label = "P95 Latency (ms)"
        title_suffix = "P95"

    csv_file = "all_stats.csv"

    timestamps = []
    vm_requests = []
    srv_requests = []
    vm_latencies = []
    srv_latencies = []

    # 3) Read the CSV file
    with open(csv_file, "r", newline="", encoding="utf-8") as f:
        reader = csv.reader(f)
        header = next(reader, None)  # attempt to read a header row

        for row in reader:
            # We expect at least 8 columns, skip if malformed
            if len(row) < 8:
                continue

            # row[0] => Timestamp
            ts_str = row[0].strip()
            if ts_str.endswith("Z"):
                ts_str = ts_str[:-1]  # remove trailing 'Z' so fromisoformat can handle it

            try:
                ts = datetime.datetime.fromisoformat(ts_str)
            except ValueError:
                continue  # skip rows with invalid timestamp

            # row[1] => VMRequests
            # row[2] => ServerlessRequests
            try:
                vm_req = int(row[1])
                sr_req = int(row[2])
            except ValueError:
                continue

            vm_requests.append(vm_req)
            srv_requests.append(sr_req)
            timestamps.append(ts)

            # row[x] => latencies depending on 'mode'
            vm_latencies.append(parse_latency_ms(row[vm_lat_col]))
            srv_latencies.append(parse_latency_ms(row[srv_lat_col]))

    if not timestamps:
        print("No valid records found in CSV. Exiting.")
        sys.exit(0)

    # 4) Create two y-axes
    x_positions = range(len(timestamps))

    fig, ax1 = plt.subplots(figsize=(12, 6))
    ax2 = ax1.twinx()  # secondary y-axis

    # --- Primary y-axis (stacked bars) ---
    ax1.bar(x_positions, vm_requests, label="VM Requests", color="steelblue")
    ax1.bar(x_positions, srv_requests, bottom=vm_requests, label="Serverless Requests", color="darkorange")
    ax1.set_ylabel("Number of Requests")

    # --- Secondary y-axis (latencies) ---
    ax2.plot(x_positions, vm_latencies, label="VM " + title_suffix, color="black")
    ax2.plot(x_positions, srv_latencies, label="Serverless " + title_suffix, color="red")
    ax2.set_ylabel(lat_label)

    # Format the x-axis with timestamps
    plt.xticks(
        list(x_positions),
        [ts.strftime("%H:%M:%S") for ts in timestamps],
        rotation=45,
        ha="right"
    )
    ax1.set_xlabel("Timestamp")

    # Combine legends
    bars_legends, bars_labels = ax1.get_legend_handles_labels()
    lines_legends, lines_labels = ax2.get_legend_handles_labels()
    ax1.legend(bars_legends + lines_legends, bars_labels + lines_labels, loc="best")

    plt.title(f"Requests (Stacked) and {title_suffix} Latency on Secondary Axis")
    plt.tight_layout()
    plt.savefig("req_with_latency.png")
    plt.show()

if __name__ == "__main__":
    main()
