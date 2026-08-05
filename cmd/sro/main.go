package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ease-lab/Spandana/internal/costcalc"
)

// --------------------------------------------------------
// 1) Direct Metric Ingestion State (1-Millisecond Buckets)
// --------------------------------------------------------

var (
	// Map of absolute Unix Time (Milliseconds) -> Total Requests Cluster-wide
	metricsMap   = make(map[int64]int64)
	metricsMutex sync.Mutex
)

// HighResPayload defines the JSON structure sidecars must use to push 1ms metrics.
type HighResPayload struct {
	DeploymentName   string  `json:"deploymentName"`
	StartTimestampMs int64   `json:"startTimestampMs"`
	Data             []int64 `json:"data"` // Array of 1ms request counts
}

// --------------------------------------------------------
// 2) Configuration
// --------------------------------------------------------

type Config struct {
	DeploymentName             string  `json:"deploymentName"`
	ExecutionTimeMs            float64 `json:"executionTimeMs"`
	MemoryMB                   float64 `json:"memoryMB"`
	RPSPerUnitOfScaling        float64 `json:"rpsPerUnitOfScaling"`
	ApplicationContainerCPU    int64   `json:"appCPU"`
	HelperCPU                  int64   `json:"helperCPU"`
	ScaleTickerIntervalSeconds int64   `json:"scaleInterval"`
	QueueSize                  int64   `json:"queueSize"` // Bounded queue size per pod
	IngestAddress              string  `json:"ingestAddress"`
}

var cfg Config

func loadConfig(path string) (Config, error) {
	var config Config
	file, err := os.ReadFile(path)
	if err != nil {
		return config, err
	}
	err = json.Unmarshal(file, &config)
	return config, err
}

// --------------------------------------------------------
// 3) Ingestion Handler
// --------------------------------------------------------

func ingestHighResMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload HighResPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if payload.DeploymentName != cfg.DeploymentName {
		http.Error(w, "payload deployment does not match this SRO", http.StatusConflict)
		return
	}
	if payload.StartTimestampMs <= 0 || len(payload.Data) == 0 {
		http.Error(w, "invalid or empty metrics payload", http.StatusBadRequest)
		return
	}
	metricsMutex.Lock()
	defer metricsMutex.Unlock()

	// Align the incoming array with absolute Unix milliseconds
	for i, requests := range payload.Data {
		if requests > 0 {
			targetBucket := payload.StartTimestampMs + int64(i)
			// Accumulate data across all sidecars for this exact millisecond
			metricsMap[targetBucket] += requests
		}
	}

	w.WriteHeader(http.StatusOK)
}

// --------------------------------------------------------
// 4) High-Resolution Exact Virtual Queue Simulator
// --------------------------------------------------------

// simulateExactQueue1ms evaluates the bounded queue and leaky bucket at exact 1ms precision.
// It returns 1-second aggregates of the OFFLOADS to pass into the external cost calculator.
func simulateExactQueue1ms(buffer1ms []int64, nPods int64, queueSizePerPod int64, rpsPerPod int64, startSecUnix int64) []costcalc.LoadPoint {
	numSeconds := len(buffer1ms) / 1000 // 1000 buckets of 1ms = 1 second
	residual1s := make([]costcalc.LoadPoint, numSeconds)

	if nPods == 0 {
		for sec := 0; sec < numSeconds; sec++ {
			var sum int64 = 0
			for ms := 0; ms < 1000; ms++ {
				sum += buffer1ms[sec*1000+ms]
			}
			residual1s[sec] = costcalc.LoadPoint{
				Timestamp: time.Unix(startSecUnix+int64(sec), 0),
				Rps:       sum,
			}
		}
		return residual1s
	}

	maxQueue := float64(nPods * queueSizePerPod)
	// Example: 150 RPS = 0.15 requests drained per 1ms
	drainPer1ms := float64(nPods*rpsPerPod) / 1000.0
	var currentQueue float64 = 0

	for sec := 0; sec < numSeconds; sec++ {
		var offloadsThisSec float64 = 0

		for ms := 0; ms < 1000; ms++ {
			arrivals := float64(buffer1ms[sec*1000+ms])

			// 1. Add new arrivals to the queue
			currentQueue += arrivals

			// 2. Check for microburst queue overflow
			if currentQueue > maxQueue {
				overflow := currentQueue - maxQueue
				offloadsThisSec += overflow
				currentQueue = maxQueue
			}

			// 3. Drain the leaky bucket for this 1ms interval
			currentQueue -= drainPer1ms
			if currentQueue < 0 {
				currentQueue = 0
			}
		}

		// Record the total exact offloads that occurred in this 1-second window
		residual1s[sec] = costcalc.LoadPoint{
			Timestamp: time.Unix(startSecUnix+int64(sec), 0),
			Rps:       int64(math.Round(offloadsThisSec)),
		}
	}

	return residual1s
}

// --------------------------------------------------------
// 5) Core Optimizer Logic
// --------------------------------------------------------

func computeBestThreshold(
	buffer1ms []int64,
	loadPattern1s []costcalc.LoadPoint,
	maxRps int64,
	podRPSCapacity int64,
	podSizeMilicores int64,
	executionTimeMs float64,
	memoryMB float64,
	startSecUnix int64,
) (bestPodCount int64, bestTotalCost float64, podCosts, lambdaCosts, totalCosts []float64) {

	derivedMaxPods := (maxRps + podRPSCapacity - 1) / podRPSCapacity
	if derivedMaxPods < 0 {
		derivedMaxPods = 0
	}

	// Add padding to ensure the search covers the U-curve during extreme microbursts
	searchMaxPods := derivedMaxPods + 5

	podCosts = make([]float64, searchMaxPods+1)
	lambdaCosts = make([]float64, searchMaxPods+1)
	totalCosts = make([]float64, searchMaxPods+1)

	bestPodCount = 0
	bestTotalCost = math.Inf(1)

	qSize := cfg.QueueSize
	if qSize == 0 {
		qSize = 15 // Default queue size
	}

	for t := int64(1); t <= searchMaxPods; t++ {
		// A. Get standard base Pod cost
		pCost, _ := costcalc.ComputePodAndLambdaCost(
			loadPattern1s,
			t,
			podSizeMilicores,
			podRPSCapacity,
			executionTimeMs,
			memoryMB,
		)

		// B. Run 1ms Exact Virtual Queue Simulation to find TRUE offloads
		residual1s := simulateExactQueue1ms(buffer1ms, t, qSize, podRPSCapacity, startSecUnix)

		// C. Bill the TRUE offloads as Lambda Cost (Pass 0 pods to force pure serverless billing)
		_, lCost := costcalc.ComputePodAndLambdaCost(
			residual1s,
			0,
			podSizeMilicores,
			podRPSCapacity,
			executionTimeMs,
			memoryMB,
		)

		total := pCost + lCost

		log.Printf(
			"Cost combination - Pods: %d, Pod Cost: %.6f, Lambda Cost: %.6f, Total Cost: %.6f",
			t,
			pCost,
			lCost,
			total,
		)

		podCosts[t] = pCost
		lambdaCosts[t] = lCost
		totalCosts[t] = total

		if total < bestTotalCost {
			bestTotalCost = total
			bestPodCount = t
		} else if total > bestTotalCost {
			// Early stopping: Cost curve is going up
			break
		}
	}

	return
}

// ---------------------------------------------------------
// 6) Main
// ---------------------------------------------------------

func main() {
	configPath := flag.String("config", "configs/sro/ratings.json", "Path to the JSON configuration file")
	flag.Parse()

	var err error
	cfg, err = loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration from %s: %v", *configPath, err)
	}

	log.Printf("MANAGING DEPLOYMENT: %s\n", cfg.DeploymentName)

	if cfg.IngestAddress == "" {
		cfg.IngestAddress = ":8087"
	}

	// Start the Ingestion HTTP Server in the background
	go func() {
		http.HandleFunc("/metrics/ingest", ingestHighResMetrics)
		log.Printf("Starting 1ms-resolution push-ingest server on %s", cfg.IngestAddress)
		if err := http.ListenAndServe(cfg.IngestAddress, nil); err != nil {
			log.Fatalf("Ingest server failed: %v", err)
		}
	}()

	outputDir := os.Getenv("SPANDANA_OUTPUT_DIR")
	if outputDir == "" {
		outputDir = "artifacts/sro"
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v\n", err)
	}

	logFileName := fmt.Sprintf("%s.log", cfg.DeploymentName)
	logFilePath := filepath.Join(outputDir, logFileName)
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("Failed to open log file: %v\n", err)
	}
	defer logFile.Close()

	multiWriter := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(multiWriter)

	tickerInterval := time.Duration(cfg.ScaleTickerIntervalSeconds) * time.Second

	statsFilePath := filepath.Join(outputDir, "all_stats.csv")
	statsFile, err := os.OpenFile(statsFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Fatalf("Failed to open/create all_stats.csv: %v\n", err)
	}
	defer statsFile.Close()

	costSummaryFilePath := filepath.Join(outputDir, "cost_summary.csv")
	costSummaryFile, err := os.OpenFile(costSummaryFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Fatalf("Failed to open/create cost_summary.csv: %v\n", err)
	}
	defer costSummaryFile.Close()

	if _, err := costSummaryFile.WriteString("Timestamp,VmCostThisTick,LambdaCostThisTick,TotalCostThisTick\n"); err != nil {
		log.Fatalf("Failed to write cost summary header: %v", err)
	}

	resourceDecisionsFilePath := filepath.Join(outputDir, "resource_decisions.csv")
	resourceDecisionsFile, err := os.OpenFile(resourceDecisionsFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Fatalf("Failed to open/create resource_decisions.csv: %v\n", err)
	}
	defer resourceDecisionsFile.Close()

	if _, err := resourceDecisionsFile.WriteString("Timestamp,CurrentPodCount,NewPodCount\n"); err != nil {
		log.Fatalf("Failed to write resource decisions header: %v", err)
	}

	now := time.Now()
	next := now.Truncate(time.Second).Add(time.Second)
	time.Sleep(time.Until(next))

	ticker := time.NewTicker(tickerInterval)
	defer ticker.Stop()

	log.Printf("SRO running, aggregating pushed 1ms metrics every %s...\n", tickerInterval)

	for range ticker.C {

		// ---------------------------------------------------------
		// STEP 1) Extract Map Window and Build LoadPattern
		// ---------------------------------------------------------
		// Keep the pipeline settling time to ensure all sidecars have finished batching
		pipelineDelayMs := int64(15000)
		evaluationEndMs := time.Now().UnixMilli() - pipelineDelayMs
		startMs := evaluationEndMs - (cfg.ScaleTickerIntervalSeconds * 1000)
		startSecUnix := startMs / 1000

		totalBuckets := cfg.ScaleTickerIntervalSeconds * 1000 // 120,000 items
		buffer1ms := make([]int64, totalBuckets)

		var loadPattern []costcalc.LoadPoint
		var maxRps int64 = 0

		metricsMutex.Lock()
		// Extract raw 1ms data
		for i := int64(0); i < totalBuckets; i++ {
			msKey := startMs + i
			buffer1ms[i] = metricsMap[msKey]

			// Free processed memory
			delete(metricsMap, msKey)
		}

		// Garbage collect any out-of-bounds packets that arrived extremely late
		for k := range metricsMap {
			if k < startMs {
				delete(metricsMap, k)
			}
		}
		metricsMutex.Unlock()

		// Aggregate the 1ms data into 1s LoadPoints for maxRps and logging
		for sec := int64(0); sec < cfg.ScaleTickerIntervalSeconds; sec++ {
			var rpsSum int64 = 0
			for ms := int64(0); ms < 1000; ms++ {
				rpsSum += buffer1ms[sec*1000+ms]
			}

			timestampSec := startSecUnix + sec
			loadPattern = append(loadPattern, costcalc.LoadPoint{
				Timestamp: time.Unix(timestampSec, 0),
				Rps:       rpsSum,
			})

			if rpsSum > maxRps {
				maxRps = rpsSum
			}
		}

		fmt.Println("Extracted and Aggregated Load Pattern:")
		for _, lp := range loadPattern {
			log.Printf("Aggregated load point - Timestamp: %s, RPS: %d", lp.Timestamp.Format(time.RFC3339), lp.Rps)

			statsRow := fmt.Sprintf("%s,%d\n", lp.Timestamp.Format(time.RFC3339), lp.Rps)
			statsFile.WriteString(statsRow)
		}
		fmt.Println("-----")
		log.Printf("Max RPS in this batch: %d\n", maxRps)

		// ---------------------------------------------------------
		// STEP 2) Execute Optimization
		// ---------------------------------------------------------
		podSizeMilicores := cfg.ApplicationContainerCPU + cfg.HelperCPU

		bestPodCount, bestTotalCost, podCosts, lambdaCosts, _ := computeBestThreshold(
			buffer1ms,
			loadPattern,
			maxRps,
			int64(cfg.RPSPerUnitOfScaling),
			int64(podSizeMilicores),
			cfg.ExecutionTimeMs,
			cfg.MemoryMB,
			startSecUnix,
		)

		log.Printf(
			"Best # of pods = %d, total cost = %.6f, (pod cost=%.6f, lambda cost=%.6f)",
			bestPodCount,
			bestTotalCost,
			podCosts[bestPodCount],
			lambdaCosts[bestPodCount],
		)

		// ---------------------------------------------------------
		// STEP 3) Execute Scaling
		// ---------------------------------------------------------
		latestReplicasURL := fmt.Sprintf("http://localhost:9000/metrics/latest-replicas?namespace=default&deploymentName=%s", cfg.DeploymentName)

		type LatestReplicasResponse struct {
			Status            string `json:"status"`
			Namespace         string `json:"namespace"`
			DeploymentName    string `json:"deploymentName"`
			DesiredReplicas   int    `json:"desiredReplicas"`
			AvailableReplicas int    `json:"availableReplicas"`
			ReadyReplicas     int    `json:"readyReplicas"`
			RunningPods       int    `json:"runningPods"`
		}

		var lr LatestReplicasResponse
		respReplicas, err := http.Get(latestReplicasURL)
		if err == nil && respReplicas.StatusCode == http.StatusOK {
			bodyBytes, _ := io.ReadAll(respReplicas.Body)
			respReplicas.Body.Close()
			json.Unmarshal(bodyBytes, &lr)
		}

		currentPodCount := int64(lr.RunningPods)
		desiredPodCount := int64(bestPodCount)
		if desiredPodCount == 0 {
			desiredPodCount = 1
		}

		if desiredPodCount != currentPodCount {
			log.Printf("Scaling from %d pods to %d pods...", currentPodCount, desiredPodCount)
			scaleURL := fmt.Sprintf(
				"http://localhost:9000/metrics/patch-replicas?namespace=default&deploymentName=%s&replicas=%d",
				cfg.DeploymentName, desiredPodCount,
			)
			respScale, _ := http.Get(scaleURL)
			if respScale != nil {
				respScale.Body.Close()
			}
		}

		// ---------------------------------------------------------
		// STEP 4) Flush Files
		// ---------------------------------------------------------
		costSummaryFile.WriteString(fmt.Sprintf("%s,%.6f,%.6f,%.6f\n", time.Now().Format(time.RFC3339), podCosts[desiredPodCount], lambdaCosts[desiredPodCount], bestTotalCost))
		resourceDecisionsFile.WriteString(fmt.Sprintf("%s,%d,%d\n", time.Now().Format(time.RFC3339), currentPodCount, desiredPodCount))

		statsFile.Sync()
		costSummaryFile.Sync()
		resourceDecisionsFile.Sync()
	}
}
