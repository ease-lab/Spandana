#!/usr/bin/env python3

import sys
import csv

def main():
    if len(sys.argv) < 2:
        print("Usage: python cost_calculator.py <csv_file> [true/false]")
        sys.exit(1)

    csv_file = sys.argv[1]
    # Omit first row if the second argument is "1"
    skip_first = (len(sys.argv) >= 3 and sys.argv[2] == "1")

    total_vm_cost = 0.0
    total_lambda_cost = 0.0

    with open(csv_file, "r", newline="") as f:
        reader = csv.DictReader(f)
        first_data_row_skipped = False

        for row in reader:
            # Skip the first data row if requested
            if skip_first and not first_data_row_skipped:
                first_data_row_skipped = True
                continue

            total_vm_cost += float(row["VmCostThisTick"])
            total_lambda_cost += float(row["LambdaCostThisTick"])

    total_cost = total_vm_cost + total_lambda_cost

    print(f"Total VM Cost: {total_vm_cost}")
    print(f"Total Serverless (Lambda) Cost: {total_lambda_cost}")
    print(f"Total Cost: {total_cost}")

if __name__ == "__main__":
    main()
