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
        print("Usage: python plot_latency.py [median|p95]")
        sys.exit(1)
    
    mode = sys.argv[1].lower()
    if mode not in ("median", "p95"):
        print("Invalid mode. Must be either 'median' or 'p95'.")
        sys.exit(1)

    csv_file = "all_stats.csv"

    timestamps = []
    vm_latencies = []
    srv_latencies = []

    # According to your CSV columns:
    #   0 = Timestamp (RFC3339)
    #   1 = VMRequests
    #   2 = ServerlessRequests
    #   3 = VMMedianLatency
    #   4 = VMP95Latency
    #   5 = ServerlessMedianLatency
    #   6 = ServerlessP95Latency
    #   7 = RateLimit

    # We'll choose which columns to use based on the command-line param:
    if mode == "median":
        vm_col = 3  # VMMedianLatency
        srv_col = 5 # ServerlessMedianLatency
    else:  # "p95"
        vm_col = 4  # VMP95Latency
        srv_col = 6 # ServerlessP95Latency

    # 1. Read CSV
    with open(csv_file, "r", newline="", encoding="utf-8") as f:
        reader = csv.reader(f)
        # Attempt to read header (if the file has one)
        header = next(reader, None)
        
        # If the header doesn't look like we expect, 
        # you might want to check or move the file pointer back.
        
        # 2. Parse each row
        for row in reader:
            if len(row) < 8:
                continue  # skip malformed lines

            # row[0] = timestamp in RFC3339 (e.g., "2025-03-02T10:00:00Z")
            ts_str = row[0]
            try:
                # fromisoformat won't parse the trailing 'Z' directly, so let's handle that 
                # or you can do more robust parsing if your timestamps always end with 'Z'.
                if ts_str.endswith("Z"):
                    ts_str = ts_str[:-1]  # remove 'Z'
                ts = datetime.datetime.fromisoformat(ts_str)
            except ValueError:
                # skip any row with a bad timestamp
                continue

            vm_lat_str = row[vm_col]
            srv_lat_str = row[srv_col]

            vm_lat = parse_latency_ms(vm_lat_str)
            srv_lat = parse_latency_ms(srv_lat_str)

            timestamps.append(ts)
            vm_latencies.append(vm_lat)
            srv_latencies.append(srv_lat)

    if not timestamps:
        print("No valid data found or CSV is empty.")
        sys.exit(0)

    # 3. Plot
    plt.figure(figsize=(10, 5))

    # We can just plot them as lines over time
    plt.plot(timestamps, vm_latencies, label="VM "+mode.title()+" Latency (ms)", color="blue")
    plt.plot(timestamps, srv_latencies, label="Serverless "+mode.title()+" Latency (ms)", color="orange")

    # Format the X-axis
    plt.xlabel("Timestamp")
    plt.ylabel("Latency (ms)")
    plt.title(f"VM vs Serverless {mode.title()} Latencies Over Time")

    # Rotate x-axis labels if needed
    plt.xticks(rotation=45, ha="right")

    # We can let matplotlib choose a decent date formatting
    # but to improve clarity with many data points, you might do:
    plt.gcf().autofmt_xdate()

    plt.legend()
    plt.tight_layout()
    plt.savefig('lat.png')
    plt.show()

if __name__ == "__main__":
    main()
