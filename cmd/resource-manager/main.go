package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/metrics/pkg/client/clientset/versioned"
)

// LabelBasedTarget represents a (namespace, labelSelector, containerName) grouping.
type LabelBasedTarget struct {
	Namespace     string `json:"namespace"`
	LabelSelector string `json:"labelSelector"`
	ContainerName string `json:"containerName"`
}

// CPUData holds the CPU limit info and CPU usage for a single Pod at a specific time.
type CPUData struct {
	PodName   string    `json:"podName"`
	CPULimit  string    `json:"cpuLimit"`
	CPUUsage  string    `json:"cpuUsage"`
	FetchedAt time.Time `json:"fetchedAt"`
}

// labelTargetResult aggregates a target’s metadata and all CPUData for the matching Pods.
type labelTargetResult struct {
	LabelBasedTarget
	Pods []CPUData `json:"pods"`
}

// We store *historical* data for each LabelBasedTarget in a slice of CPUData (appended over time).
var (
	cpuStore = make(map[LabelBasedTarget][]CPUData)
	mu       sync.RWMutex
)

// updateInterval defines how frequently we refresh CPU data from the cluster.
const updateInterval = 15 * time.Second

// Our sample label-based targets.
// In real usage, load from config, environment, or CLI arguments.
var targets = []LabelBasedTarget{
	{
		Namespace:     "default",
		LabelSelector: "app=reviews",
		ContainerName: "reviews",
	},
	{
		Namespace:     "default",
		LabelSelector: "app=reviews",
		ContainerName: "proxy",
	},
	{
		Namespace:     "default",
		LabelSelector: "app=ratings",
		ContainerName: "ratings",
	},
	{
		Namespace:     "default",
		LabelSelector: "app=ratings",
		ContainerName: "proxy",
	},
	{
		Namespace:     "default",
		LabelSelector: "app=productpage",
		ContainerName: "productpage",
	},
	{
		Namespace:     "default",
		LabelSelector: "app=productpage",
		ContainerName: "proxy",
	},
	{
		Namespace:     "default",
		LabelSelector: "app=details",
		ContainerName: "details",
	},
	{
		Namespace:     "default",
		LabelSelector: "app=details",
		ContainerName: "proxy",
	},
	{
		Namespace:     "default",
		LabelSelector: "app=compression",
		ContainerName: "compression",
	},
	{
		Namespace:     "default",
		LabelSelector: "app=compression",
		ContainerName: "proxy",
	},
	{
		Namespace:     "default",
		LabelSelector: "app=imgproc",
		ContainerName: "imgproc",
	},
	{
		Namespace:     "default",
		LabelSelector: "app=imgproc",
		ContainerName: "proxy",
	},
}

func initLogger() {
	logPath := os.Getenv("SPANDANA_RESOURCE_LOG")
	if logPath == "" {
		logPath = "artifacts/resource-manager/cpu_metrics.log"
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		log.Fatalf("Failed to create log directory: %v", err)
	}

	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}

	// Set log output to the file (optional: use io.MultiWriter to also keep stdout)
	log.SetOutput(logFile)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

// -----------------------------------------------------------------------------
// (2) HORIZONTAL SCALING: patch-replicas endpoint (NEW)
// -----------------------------------------------------------------------------

// scaleDeployment updates the .spec.replicas of the given Deployment. (NEW)
func scaleDeployment(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	namespace string,
	deploymentName string,
	replicas int32,
) error {
	// Option A: Using the Scale subresource
	scale, err := clientset.AppsV1().Deployments(namespace).GetScale(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get Scale subresource for deployment %q: %w", deploymentName, err)
	}

	scale.Spec.Replicas = replicas

	_, err = clientset.AppsV1().Deployments(namespace).UpdateScale(ctx, deploymentName, scale, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update Scale for deployment %q: %w", deploymentName, err)
	}

	log.Printf("Scaled deployment %q in namespace %q to %d replicas", deploymentName, namespace, replicas)
	return nil
}

// scaleDeploymentHandler is an HTTP handler that scales a Deployment horizontally. (NEW)
// Example usage:
//
//	GET /metrics/patch-replicas?namespace=default&deploymentName=hello-world-deployment&replicas=3
func scaleDeploymentHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	namespace := q.Get("namespace")
	deploymentName := q.Get("deploymentName")
	replicasStr := q.Get("replicas")

	if namespace == "" || deploymentName == "" || replicasStr == "" {
		http.Error(w, "Missing required query param: namespace, deploymentName, or replicas", http.StatusBadRequest)
		return
	}

	replicasInt, err := strconv.Atoi(replicasStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid replicas param %q: %v", replicasStr, err), http.StatusBadRequest)
		return
	}
	desiredReplicas := int32(replicasInt)

	// Build kubeconfig & clientset
	kubeconfigPath := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to build kubeconfig: %v", err), http.StatusInternalServerError)
		return
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to create clientset: %v", err), http.StatusInternalServerError)
		return
	}

	// Perform the horizontal scaling
	if err := scaleDeployment(ctx, clientset, namespace, deploymentName, desiredReplicas); err != nil {
		http.Error(w, fmt.Sprintf("Failed to scale deployment: %v", err), http.StatusInternalServerError)
		return
	}

	// Success
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          "success",
		"namespace":       namespace,
		"deploymentName":  deploymentName,
		"replicasDesired": desiredReplicas,
	})
}

// -----------------------------------------------------------------------------
// (3) Original Metrics/CPU usage code
// -----------------------------------------------------------------------------

func fetchCPULimits(
	ctx context.Context,
	clientset *kubernetes.Clientset,
	metricsClient *versioned.Clientset,
) {
	for _, target := range targets {
		// List the Pods based on the label selector
		pods, err := clientset.CoreV1().Pods(target.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: target.LabelSelector,
		})
		if err != nil {
			data := CPUData{
				PodName:   "N/A",
				CPULimit:  fmt.Sprintf("Error listing pods: %v", err),
				CPUUsage:  "",
				FetchedAt: time.Now(),
			}
			mu.Lock()
			cpuStore[target] = append(cpuStore[target], data)
			mu.Unlock()
			continue
		}

		// For each matching Pod, retrieve CPU limit & usage
		for _, pod := range pods.Items {
			// Query only running pods
			if pod.Status.Phase != v1.PodRunning {
				continue
			}
			limit, errLimit := getContainerCPULimitFromPod(pod, target.ContainerName)
			usage, errUsage := getContainerCPUUsageFromPod(ctx, metricsClient, target.Namespace, pod.Name, target.ContainerName)

			record := CPUData{
				PodName:   pod.Name,
				CPULimit:  "-",
				CPUUsage:  "-",
				FetchedAt: time.Now(),
			}

			if errLimit != nil {
				record.CPULimit = fmt.Sprintf("Error: %v", errLimit)
			} else {
				record.CPULimit = limit
			}

			if errUsage != nil {
				record.CPUUsage = fmt.Sprintf("Error: %v", errUsage)
			} else {
				record.CPUUsage = usage
			}

			log.Printf("Fetched CPU metrics | Namespace: %s | Pod: %s | Container: %s | Limit: %s | Usage: %s",
				target.Namespace, pod.Name, target.ContainerName, record.CPULimit, record.CPUUsage)

			mu.Lock()
			cpuStore[target] = append(cpuStore[target], record)
			mu.Unlock()
		}
	}

	pruneOldData()
}

// pruneOldData removes any CPUData records older than 17 seconds from cpuStore.
func pruneOldData() {
	cutoff := time.Now().Add(-17 * time.Second)
	mu.Lock()
	defer mu.Unlock()

	for target, records := range cpuStore {
		var pruned []CPUData
		for _, rec := range records {
			if rec.FetchedAt.After(cutoff) {
				pruned = append(pruned, rec)
			}
		}
		cpuStore[target] = pruned
	}
}

func getContainerCPULimitFromPod(pod v1.Pod, containerName string) (string, error) {
	for _, c := range pod.Spec.Containers {
		if c.Name == containerName {
			cpuLimitQty := c.Resources.Limits["cpu"]
			// Convert to millicores
			millicores := cpuLimitQty.MilliValue()
			return fmt.Sprintf("%dm", millicores), nil
		}
	}
	return "", fmt.Errorf("container %q not found in pod %q", containerName, pod.Name)
}

func getContainerCPUUsageFromPod(
	ctx context.Context,
	metricsClient *versioned.Clientset,
	namespace, podName, containerName string,
) (string, error) {

	podMetrics, err := metricsClient.MetricsV1beta1().PodMetricses(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("could not fetch metrics for Pod %s/%s: %w", namespace, podName, err)
	}

	for _, c := range podMetrics.Containers {
		if c.Name == containerName {
			usage := c.Usage.Cpu()
			if usage == nil {
				return "", fmt.Errorf("cpu usage metric not available for container %q", containerName)
			}
			millicores := usage.MilliValue()
			return fmt.Sprintf("%dm", millicores), nil
		}
	}
	return "", fmt.Errorf("container %q not found in metrics for pod %q", containerName, podName)
}

// metricsHandler dumps *all* historical data as JSON.
func metricsHandler(w http.ResponseWriter, r *http.Request) {
	mu.RLock()
	defer mu.RUnlock()

	var results []labelTargetResult
	for target, podData := range cpuStore {
		results = append(results, labelTargetResult{
			LabelBasedTarget: target,
			Pods:             podData,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(results)
}

// queryMetricsHandler queries by label_name + container_name + time range or last X minutes
func queryMetricsHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	requestedNamespace := q.Get("namespace")
	requestedLabelSel := q.Get("labelSelector")
	requestedContainer := q.Get("containerName")

	lastStr := q.Get("last")
	startTime, endTime := time.Time{}, time.Now()

	if lastStr != "" {
		minutes, err := strconv.Atoi(lastStr)
		if err != nil {
			log.Printf("Invalid last param %q, defaulting to 5 minutes. Error: %v", lastStr, err)
			minutes = 5
		}
		startTime = endTime.Add(-time.Duration(minutes) * time.Minute)
	} else {
		startTime, endTime = parseTimeRange(q)
	}

	target := LabelBasedTarget{
		Namespace:     requestedNamespace,
		LabelSelector: requestedLabelSel,
		ContainerName: requestedContainer,
	}

	mu.RLock()
	allData, exists := cpuStore[target]
	mu.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusNotFound)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]labelTargetResult{})
		return
	}

	var filtered []CPUData
	for _, record := range allData {
		if (record.FetchedAt.After(startTime) || record.FetchedAt.Equal(startTime)) &&
			(record.FetchedAt.Before(endTime) || record.FetchedAt.Equal(endTime)) {
			filtered = append(filtered, record)
		}
	}

	result := labelTargetResult{
		LabelBasedTarget: target,
		Pods:             filtered,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func parseTimeRange(q url.Values) (time.Time, time.Time) {
	startStr := q.Get("start")
	endStr := q.Get("end")

	var startTime time.Time
	var endTime time.Time

	if startStr == "" {
		startTime = time.Unix(0, 0)
	} else {
		parsed, err := time.Parse(time.RFC3339, startStr)
		if err != nil {
			log.Printf("Invalid start time %q, using epoch: %v", startStr, err)
			startTime = time.Unix(0, 0)
		} else {
			startTime = parsed
		}
	}

	if endStr == "" {
		endTime = time.Now()
	} else {
		parsed, err := time.Parse(time.RFC3339, endStr)
		if err != nil {
			log.Printf("Invalid end time %q, using now: %v", endStr, err)
			endTime = time.Now()
		} else {
			endTime = parsed
		}
	}

	if startTime.After(endTime) {
		startTime, endTime = endTime, startTime
	}

	return startTime, endTime
}

// latestReplicasHandler is an HTTP handler that returns both the
// *desired number of replicas* (from Deployment.Spec) AND the
// *currently running number of Pods* that belong to that Deployment.
//
// Example usage:
//
//	GET /metrics/latest-replicas?namespace=default&deploymentName=hello-world-deployment
func latestReplicasHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	namespace := q.Get("namespace")
	deploymentName := q.Get("deploymentName")

	if namespace == "" || deploymentName == "" {
		http.Error(w, "Missing required query param: namespace or deploymentName", http.StatusBadRequest)
		return
	}

	// Build kubeconfig & clientset
	kubeconfigPath := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to build kubeconfig: %v", err), http.StatusInternalServerError)
		return
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to create clientset: %v", err), http.StatusInternalServerError)
		return
	}

	// 1) Retrieve the Deployment
	deployment, err := clientset.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to GET deployment %q in ns %q: %v",
			deploymentName, namespace, err), http.StatusInternalServerError)
		return
	}

	// The "desired" replicas is in deployment.Spec.Replicas (pointer). Default to 1 if nil.
	var desired int32 = 1
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}

	// Available/Ready replicas from Deployment’s status
	available := deployment.Status.AvailableReplicas
	ready := deployment.Status.ReadyReplicas

	// 2) Construct the labelSelector based on the Deployment’s Pod selector
	// This is typically stored in deployment.Spec.Selector.MatchLabels,
	// but you need to convert it to a string for ListOptions.
	matchLabels := deployment.Spec.Selector.MatchLabels
	labelSelector := metav1.FormatLabelSelector(&metav1.LabelSelector{MatchLabels: matchLabels})

	// 3) List Pods for that Deployment’s label selector
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to list pods for deployment %q in ns %q: %v",
			deploymentName, namespace, err), http.StatusInternalServerError)
		return
	}

	// 4) Count how many Pods are actually in state=Running
	runningCount := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == v1.PodRunning {
			runningCount++
		}
	}

	// Build a JSON response
	response := map[string]interface{}{
		"status":            "success",
		"namespace":         namespace,
		"deploymentName":    deploymentName,
		"desiredReplicas":   desired,
		"availableReplicas": available,
		"readyReplicas":     ready,
		"runningPods":       runningCount, // The actual count of Pods in Running phase
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// -----------------------------------------------------------------------------
// main()
// -----------------------------------------------------------------------------

func main() {
	// Build kubeconfig from local file. For in-cluster usage, use rest.InClusterConfig().
	kubeconfigPath := os.Getenv("KUBECONFIG")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		log.Fatalf("Failed to build kubeconfig: %v", err)
	}

	// Create the Kubernetes client
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	// Create the Metrics client to fetch CPU usage
	metricsClient, err := versioned.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create metrics client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	initLogger()

	// Fetch CPU limits & usage once on startup
	fetchCPULimits(ctx, clientset, metricsClient)

	// Periodically refresh CPU data
	go func() {
		ticker := time.NewTicker(updateInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fetchCPULimits(ctx, clientset, metricsClient)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Graceful shutdown on SIGINT / SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("Shutting down gracefully...")
		cancel()
		os.Exit(0)
	}()

	// NEW horizontal-scaling endpoint
	http.HandleFunc("/metrics/latest-replicas", latestReplicasHandler)
	http.HandleFunc("/metrics/patch-replicas", scaleDeploymentHandler)

	// Other existing endpoints
	http.HandleFunc("/metrics", metricsHandler)
	http.HandleFunc("/metrics/query", queryMetricsHandler)

	fmt.Println("Serving metrics on :9000")
	if err := http.ListenAndServe(":9000", nil); err != nil {
		fmt.Printf("HTTP server error: %v", err)
	}
}
