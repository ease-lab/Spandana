#!/usr/bin/env python3

import csv
import matplotlib.pyplot as plt

def generate_rps_csv(input_file='all_stats.csv', output_file='rps_output.csv'):
    """
    Generates an RPS (Requests Per Second) CSV from the input stats CSV.
    The input CSV (all_stats.csv) is expected to have columns: "VMRequests" and "ServerlessRequests".
    The output CSV (rps_output.csv) will contain columns: "timestamp" and "RPS".
    """
    with open(input_file, 'r', newline='') as infile, open(output_file, 'w', newline='') as outfile:
        reader = csv.DictReader(infile)
        
        # Prepare CSV writer with a simple header
        writer = csv.writer(outfile)
        writer.writerow(["timestamp", "RPS"])
        
        # A counter to represent timestamp as an integer (starting from 1)
        counter = 0
        
        for row in reader:
            counter += 1
            # Convert VMRequests and ServerlessRequests to integers and sum them
            vm_requests = int(row["VMRequests"])
            serverless_requests = int(row["ServerlessRequests"])
            rps = vm_requests + serverless_requests
            
            # Write to the new CSV: integer timestamp, RPS
            writer.writerow([counter, rps])

def read_load_pattern_from_csv(csv_file_path):
    """
    Reads a CSV file with columns: timestamp,RPS.
    Returns a list of (timestamp, RPS) pairs.
    """
    load_pattern = []
    with open(csv_file_path, 'r', newline='') as f:
        reader = csv.DictReader(f)
        for row in reader:
            timestamp = int(row['timestamp'])
            rps = int(row['RPS'])
            load_pattern.append((timestamp, rps))
    return load_pattern

def calculate_t3_xlarge_cost(total_milicore_seconds):
    """
    Convert milicore-seconds to cost for a t3.xlarge (~$0.1664/hour, 4 vCPUs).
    NOTE: Real AWS billing is per-instance-hour, not partial usage.
    """
    COST_PER_HOUR_4_VCPU = 0.1664
    TOTAL_MILICORES = 4000      # 4 vCPUs = 4000 milicores
    SECONDS_PER_HOUR = 3600

    total_milicore_seconds_per_hour = TOTAL_MILICORES * SECONDS_PER_HOUR  # 14,400,000
    cost_per_milicore_second = COST_PER_HOUR_4_VCPU / total_milicore_seconds_per_hour
    return total_milicore_seconds * cost_per_milicore_second

def calculate_lambda_cost(num_invocations, total_gb_seconds):
    """
    Standard AWS Lambda pricing (ignoring free tier):
      - $0.20 per 1M requests
      - $0.0000166667 per GB-second
    """
    PRICE_PER_1M_REQUESTS = 0.20
    PRICE_PER_GB_SECOND = 0.0000166667

    request_cost = (num_invocations / 1_000_000) * PRICE_PER_1M_REQUESTS
    compute_cost = total_gb_seconds * PRICE_PER_GB_SECOND
    return request_cost + compute_cost

def compute_vm_and_lambda_cost(load_pattern, threshold, rps_per_vcpu, exec_time_ms, memory_mb):
    """
    For a given threshold:
      - Up to `threshold` RPS is served by VM
      - Above threshold served by Lambda
    Returns (vm_cost, lambda_cost).
    """
    memory_gb = memory_mb / 1024.0
    exec_time_sec = exec_time_ms / 1000.0

    total_vm_milicore_seconds = 0.0
    total_lambda_invocations = 0
    total_lambda_gb_seconds = 0.0

    for (timestamp, rps) in load_pattern:
        vm_served_rps = threshold
        lambda_served_rps = max(0, rps - threshold)

        # VM usage
        # If 1 vCPU (1000 milicores) can handle rps_per_vcpu, then
        # X RPS needs (X / rps_per_vcpu) * 1000 milicores
        vm_milicores = (vm_served_rps / rps_per_vcpu) * 1000.0
        # Each row in the CSV is effectively 1 second
        total_vm_milicore_seconds += vm_milicores

        # Lambda usage
        total_lambda_invocations += lambda_served_rps
        total_lambda_gb_seconds += lambda_served_rps * exec_time_sec * memory_gb

    vm_cost = calculate_t3_xlarge_cost(total_vm_milicore_seconds)
    lambda_cost = calculate_lambda_cost(total_lambda_invocations, total_lambda_gb_seconds)
    return vm_cost, lambda_cost

def main():
    # 1) Generate the RPS CSV from all_stats.csv
    generate_rps_csv(input_file='all_stats.csv', output_file='rps_output.csv')
    
    # 2) READ LOAD (RPS) FROM CSV
    csv_file = "rps_output.csv"
    load_pattern = read_load_pattern_from_csv(csv_file)

    # 3) DEFINE MICROSERVICE PARAMS
    # Example microservice parameters:
    rps_per_vcpu = 100   # 1 vCPU can handle 50 RPS
    exec_time_ms = 38   # 38ms average execution time
    memory_mb = 128     # 128 MB memory for Lambda

    # Determine the max RPS in the trace to define an upper bound on threshold
    max_rps = max(rps for (_, rps) in load_pattern)

    # 4) SWEEP THRESHOLDS
    thresholds = range(max_rps + 1)  # 0 .. max_rps inclusive

    vm_costs = []
    lambda_costs = []
    total_costs = []

    best_threshold = 0
    best_total_cost = float('inf')

    for t in thresholds:
        vm_c, lambda_c = compute_vm_and_lambda_cost(
            load_pattern, t,
            rps_per_vcpu, exec_time_ms, memory_mb
        )
        combined = vm_c + lambda_c

        vm_costs.append(vm_c)
        lambda_costs.append(lambda_c)
        total_costs.append(combined)

        if combined < best_total_cost:
            best_total_cost = combined
            best_threshold = t

    # 5) Print / Summarize
    print("=== Cost vs. Threshold ===")
    for i, t in enumerate(thresholds):
        print(f"Threshold={t:3d} => VM=${vm_costs[i]:.6f}, Lambda=${lambda_costs[i]:.6f}, Total=${total_costs[i]:.6f}")

    print("\n=== Optimal Threshold ===")
    print(f"Best Threshold: {best_threshold}")
    print(f"VM cost at best threshold:      ${vm_costs[best_threshold]:.6f}")
    print(f"Lambda cost at best threshold:  ${lambda_costs[best_threshold]:.6f}")
    print(f"Total cost at best threshold:   ${total_costs[best_threshold]:.6f}")

    # 6) PLOT THE COST CURVES
    plt.figure(figsize=(8, 5))
    plt.plot(thresholds, vm_costs, label='VM Cost', color='blue')
    plt.plot(thresholds, lambda_costs, label='Lambda Cost', color='green')
    plt.plot(thresholds, total_costs, label='Total Cost', color='red', linewidth=2)

    plt.title('Cost vs. RPS Threshold')
    plt.xlabel('RPS Threshold')
    plt.ylabel('Cost (USD)')
    plt.legend()
    plt.grid(True)

    # Mark the best threshold on the plot
    plt.axvline(x=best_threshold, color='gray', linestyle='--')
    plt.text(best_threshold + 0.5, best_total_cost, 
             f'Best={best_threshold}', 
             verticalalignment='bottom',
             color='black', 
             fontsize=9)

    plt.tight_layout()
    plt.savefig('ss1.png')
    plt.show()

if __name__ == "__main__":
    main()
