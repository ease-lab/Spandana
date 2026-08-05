import re
import matplotlib.pyplot as plt
from datetime import datetime
from collections import defaultdict

# File path
log_file = "cpu_metrics.log"

# Data structure: timestamp -> list of usages
usage_data = defaultdict(list)

# Regex to match relevant log entries
pattern = re.compile(
    r"(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}).*Container: hello-world-container.*Usage: (\d+)m"
)

# Parse the log
with open(log_file, "r") as f:
    for line in f:
        match = pattern.search(line)
        if match:
            timestamp_str, usage_str = match.groups()
            timestamp = datetime.strptime(timestamp_str, "%Y/%m/%d %H:%M:%S")
            usage = int(usage_str)
            usage_data[timestamp].append(usage)

# Compute average usage per timestamp
timestamps = sorted(usage_data.keys())
avg_usages = [sum(usages) / len(usages) for t in timestamps if (usages := usage_data[t])]

# Plotting
plt.figure(figsize=(12, 6))
plt.plot(timestamps, avg_usages, marker="o")
plt.title("Average CPU Usage of hello-world-container Over Time")
plt.xlabel("Time")
plt.ylabel("Average CPU Usage (millicores)")
plt.grid(True)
plt.tight_layout()
plt.xticks(rotation=45)
plt.savefig("cpu_usage.png")
plt.show()
