1) GET /metrics
Purpose:
Dump all historical CPU usage and limits data. This includes data for every LabelBasedTarget that the service is monitoring. Data is appended each time the service fetches new metrics (periodically and on startup).

curl -X GET http://localhost:9000/metrics


2) GET /metrics/query
Purpose:
Filter the historical CPU data by:

namespace
labelSelector
containerName
Time range (either by explicit start/end timestamps, or by the last X minutes)

Last 5 minutes
curl -X GET "http://localhost:9000/metrics/query?namespace=default&labelSelector=app=hello-world-container&containerName=hello-world-container&last=5"

Time range
curl -X GET "http://localhost:9000/metrics/query?namespace=default&labelSelector=app=hello-world-container&containerName=hello-world-container&start=2025-03-04T12:00:00Z&end=2025-03-04T13:00:00Z"


Response 

{
  "namespace": "default",
  "labelSelector": "app=hello-world-container",
  "containerName": "hello-world-container",
  "pods": [
    {
      "podName": "hello-world-deploy-123",
      "cpuLimit": "250m",
      "cpuUsage": "120m",
      "fetchedAt": "2025-03-04T12:15:40.123Z"
    },
    {
      "podName": "hello-world-deploy-456",
      "cpuLimit": "500m",
      "cpuUsage": "450m",
      "fetchedAt": "2025-03-04T12:30:00.456Z"
    }
  ]
}


3) GET /metrics/latest-limit

Purpose:
Return only the most recent CPU limit for each Pod matching the provided (namespace, labelSelector, containerName). This is essentially a snapshot of the last-seen CPU limit (and usage) for each Pod rather than the entire historical record.

curl -X GET "http://localhost:9000/metrics/latest-limit?namespace=default&labelSelector=app=hello-world-container&containerName=hello-world-container"

Response :

{
  "namespace": "default",
  "labelSelector": "app=hello-world-container",
  "containerName": "hello-world-container",
  "pods": [
    {
      "podName": "hello-world-deploy-abc",
      "cpuLimit": "250m",
      "cpuUsage": "133m",
      "fetchedAt": "2025-03-04T12:55:01.000Z"
    },
    {
      "podName": "hello-world-deploy-xyz",
      "cpuLimit": "500m",
      "cpuUsage": "490m",
      "fetchedAt": "2025-03-04T12:55:02.000Z"
    }
  ]
}


4) GET /metrics/patch-limit

curl -X GET "http://localhost:9000/metrics/patch-limit?namespace=default&labelSelector=app%3Dhello-world-container&containerName=hello-world-container&cpuLimit=250m"


Response : 
{
  "status": "success",
  "message": "Patched all Pods for container=\"hello-world-container\" in namespace=\"default\" with CPU limit=\"250m\""
}
