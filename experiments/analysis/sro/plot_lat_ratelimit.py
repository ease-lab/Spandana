#!/usr/bin/env python3

import csv
import datetime
import sys
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
    if len(sys.argv) < 2:
        print("Usage: python plot_latency_ratelimit.py [median|p95]")
        sys.exit(1)
    
    mode = sys.argv[1].lower()
    if mode not in ("median", "p95"):
        print("Invalid mode. Must be either 'median' or 'p95'.")
        sys.exit(1)

    csv_file = "all_stats.csv"

    timestamps = []
    vm_latencies = []
    srv_latencies = []
    rate_limits = []

    # CSV columns (assuming the format in all_stats.csv):
    #   0) Timestamp
    #   1) VMRequests
    #   2) ServerlessRequests
    #   3) VMMedianLatency
    #   4) VMP95Latency
    #   5) ServerlessMedianLatency
    #   6) ServerlessP95Latency
    #   7) RateLimit

    # Decide which columns to read for VM/Serverless latencies
    if mode == "median":
        vm_col = 3  # VMMedianLatency
        srv_col = 5 # ServerlessMedianLatency
        lat_label = "Median Latency (ms)"
    else:  # "p95"
        vm_col = 4  # VMP95Latency
        srv_col = 6 # ServerlessP95Latency
        lat_label = "95th-Percentile Latency (ms)"

    with open(csv_file, "r", newline="", encoding="utf-8") as f:
        reader = csv.reader(f)
        header = next(reader, None)  # Attempt to read the header row

        # Read each subsequent row
        for row in reader:
            if len(row) < 8:
                continue  # skip malformed lines

            # row[0] is Timestamp (RFC3339, might end with 'Z')
            ts_str = row[0]
            if ts_str.endswith("Z"):
                ts_str = ts_str[:-1]  # remove trailing 'Z' so fromisoformat() can parse
            try:
                ts = datetime.datetime.fromisoformat(ts_str)
            except ValueError:
                # skip rows with invalid dates
                continue

            vm_lat_str = row[vm_col]
            srv_lat_str = row[srv_col]
            vm_lat = parse_latency_ms(vm_lat_str)
            srv_lat = parse_latency_ms(srv_lat_str)

            # row[7] = RateLimit
            try:
                rLimit = int(row[7])
            except ValueError:
                rLimit = 0

            timestamps.append(ts)
            vm_latencies.append(vm_lat)
            srv_latencies.append(srv_lat)
            rate_limits.append(rLimit)

    if not timestamps:
        print("No valid data found or CSV is empty.")
        sys.exit(0)

    # Create a figure with one set of x-axis, two separate y-axes
    fig, ax1 = plt.subplots(figsize=(10, 5))

    # ax1: latencies
    ax1.plot(timestamps, vm_latencies, label="VM " + mode.title(), color="blue")
    ax1.plot(timestamps, srv_latencies, label="Serverless " + mode.title(), color="orange")

    ax1.set_xlabel("Timestamp")
    ax1.set_ylabel(lat_label, color="black")

    # Rotate x-axis labels for clarity
    plt.xticks(rotation=45, ha="right")
    plt.gcf().autofmt_xdate()

    # Create a second y-axis sharing the same x-axis
    ax2 = ax1.twinx()
    ax2.plot(timestamps, rate_limits, label="RateLimit", color="green", linestyle="-")
    ax2.set_ylabel("Rate Limit (requests/sec)", color="black")

    # Combine legends for both axes
    lines1, labels1 = ax1.get_legend_handles_labels()
    lines2, labels2 = ax2.get_legend_handles_labels()
    ax1.legend(lines1 + lines2, labels1 + labels2, loc="best")

    plt.title(f"VM vs Serverless {mode.title()} Latencies & Rate Limit Changes")
    plt.tight_layout()
    plt.show()

if __name__ == "__main__":
    main()
