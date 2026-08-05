import re
import pandas as pd
import seaborn as sns
import matplotlib.pyplot as plt
import sys
import os

def analyze_cpu_logs(log_filepath):
    """
    Parses CPU utilization logs, filters for the main application container,
    and generates plots to visualize load balance and average utilization.
    Skips only error lines for the target container (keeps other containers).
    """
    log_pattern = re.compile(
        r'^(?P<timestamp>\d{4}/\d{2}/\d{2}\s\d{2}:\d{2}:\d{2}).*?'
        r'Pod: (?P<pod>[\w-]+)\s*\|\s*'
        r'Container: (?P<container>[\w-]+)\s*\|.*?'
        r'Usage: (?P<usage>\d+)m$'
    )

    if len(sys.argv) > 2:
        container_name = sys.argv[2]
    else:
        print("Error: No container name provided.")
        print("Usage: python script.py <path/to/log> <container_name>")
        return

    print(f"Reading and parsing log file: {log_filepath}")

    valid_lines = []
    try:
        with open(log_filepath, 'r') as f:
            for line in f:
                # Skip only if this line is for the target container AND contains Error
                if f"Container: {container_name}" in line and "Error:" in line:
                    continue
                valid_lines.append(line)
    except FileNotFoundError:
        print(f"Error: Log file not found at '{log_filepath}'")
        return

    if not valid_lines:
        print("No valid lines found in the log.")
        return

    parsed_data = []
    for line in valid_lines:
        match = log_pattern.match(line)
        if match:
            parsed_data.append(match.groupdict())

    if not parsed_data:
        print("No valid log data could be parsed after filtering error lines.")
        return

    df = pd.DataFrame(parsed_data)
    df['timestamp'] = pd.to_datetime(df['timestamp'], errors='coerce')
    df['usage'] = pd.to_numeric(df['usage'], errors='coerce')
    df.dropna(subset=['timestamp', 'usage'], inplace=True)

    df_app = df[df['container'] == container_name].copy()

    if df_app.empty:
        print(f"No log entries found for the '{container_name}' container.")
        return

    print(f"Found {len(df_app)} metrics for the '{container_name}' container after filtering error lines.")

    # --- Calculate overall average utilization ---
    average_usage = df_app['usage'].mean()
    print(f"\nOverall Average CPU Utilization ({container_name}): {average_usage:.2f} millicores\n")

    # --- Calculate average utilization over time ---
    df_avg_time = df_app.groupby('timestamp')['usage'].mean().reset_index()

    # --- Define output file paths ---
    log_dir = os.path.dirname(os.path.abspath(log_filepath))
    log_basename = os.path.splitext(os.path.basename(log_filepath))[0]  # e.g., "cpu_metrics"

    mean_cpu_path = os.path.join(log_dir, f'mean_{log_basename}.cpu')
    dist_plot_path = os.path.join(log_dir, f'{log_basename}_cpu_usage_distribution.png')
    time_plot_path = os.path.join(log_dir, f'{log_basename}_cpu_usage_over_time.png')
    avg_time_plot_path = os.path.join(log_dir, f'{log_basename}_average_cpu_usage_over_time.png')

    # --- Save mean.cpu file ---
    with open(mean_cpu_path, 'w') as f:
        f.write(f"{average_usage:.2f}\n")
    print(f"Saved overall average CPU utilization to: {mean_cpu_path}")
    print("\nAnalysis complete. All files saved.")


if __name__ == '__main__':
    if len(sys.argv) < 3:
        print("Usage: python script.py <path/to/cpu_metrics.log> <container_name>")
        sys.exit(1)

    log_file_path = sys.argv[1]
    analyze_cpu_logs(log_file_path)
