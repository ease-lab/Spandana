package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// ------------------------------------------------
// HighResPayload is the ten-second batch sent to the SRO.
// ------------------------------------------------
type HighResPayload struct {
	DeploymentName   string  `json:"deploymentName"`
	StartTimestampMs int64   `json:"startTimestampMs"`
	Data             []int64 `json:"data"` // Will contain 10,000 elements
}

// secondStats holds the per-second stats we log.
type secondStats struct {
	Timestamp               time.Time `json:"timestamp"`
	VMRequests              int64     `json:"vmRequests"`
	ServerlessRequests      int64     `json:"serverlessRequests"`
	VMMedianLatency         string    `json:"vmMedianLatency"`
	VMP95Latency            string    `json:"vmP95Latency"`
	ServerlessMedianLatency string    `json:"serverlessMedianLatency"`
	ServerlessP95Latency    string    `json:"serverlessP95Latency"`
	RateLimit               int       `json:"rateLimit"`
}

// Proxy is our main struct, storing configuration, ring buffer, etc.
type Proxy struct {
	// Leaky-bucket fields
	rateLimit    int
	dripMu       sync.Mutex    // guards lastDripTime
	lastDripTime time.Time     // updated after each VM request
	dripInterval time.Duration // derived from rateLimit = 1 / RPS

	// A bounded queue to limit how many requests are “waiting”.
	// If this channel is full, we fallback to serverless.
	requestQueue chan struct{}
	queueSize    int

	// The usual fields
	logFile       *os.File
	logEventsFile *os.File

	vmLatencies         []time.Duration
	serverlessLatencies []time.Duration
	latencyLock         sync.Mutex

	// Rolling 5-second window
	windowSize                int
	vmLatenciesWindow         [][]time.Duration
	serverlessLatenciesWindow [][]time.Duration
	windowIndex               int

	once             sync.Once
	firstRequestTime time.Time
	slo              time.Duration

	vmRequestsThisSecond         int64
	serverlessRequestsThisSecond int64

	consecutiveSLOViolations int
	consecutiveSLOPasses     int

	sloPassThreshold      int
	sloViolationThreshold int

	logBuffer []string
	logBufMu  sync.Mutex

	eventsBufMu     sync.Mutex
	eventsLogBuffer []string

	httpClient *http.Client

	vmBackendURL    string
	vmBackendPort   int
	serverlessURL   string
	serverlessPort  int
	proxyListenPort int

	// Millisecond-resolution arrival tracking
	muRequestCounts sync.Mutex
	requestCountsMs map[int64]int64

	// Ten-second SRO batching
	deploymentName string
	sroIngestURL   string
	pushBuffer     []int64
	pushStartMs    int64
}

func main() {
	// Seed randomizer for push jitter
	rand.Seed(time.Now().UnixNano())

	// ------------------------------------------------
	//  1) READ ENVIRONMENT VARIABLES (WITH DEFAULTS)
	// ------------------------------------------------

	podName := os.Getenv("POD_NAME")
	if podName == "" {
		podName = "unknown-pod"
	}

	vmBackendURL := os.Getenv("VM_BACKEND_URL")
	if vmBackendURL == "" {
		vmBackendURL = "http://localhost"
		fmt.Printf("VM_BACKEND_URL not set; defaulting to %s\n", vmBackendURL)
	}

	vmPortStr := os.Getenv("VM_BACKEND_PORT")
	vmPort, err := strconv.Atoi(vmPortStr)
	if err != nil || vmPort <= 0 {
		vmPort = 5000
		fmt.Printf("Invalid or missing VM_BACKEND_PORT. Defaulting to %d.\n", vmPort)
	}

	serverlessURL := os.Getenv("SERVERLESS_URL")
	if serverlessURL == "" {
		fmt.Println("SERVERLESS_URL must be set")
		return
	} else {
		fmt.Printf("Serverless URL set to %s\n", serverlessURL)
	}

	serverlessPortStr := os.Getenv("SERVERLESS_PORT")
	serverlessPort, err := strconv.Atoi(serverlessPortStr)
	if err != nil || serverlessPort <= 0 {
		serverlessPort = 4000
		fmt.Printf("Invalid or missing SERVERLESS_PORT. Defaulting to %d.\n", serverlessPort)
	}

	proxyPortStr := os.Getenv("PROXY_PORT")
	proxyPort, err := strconv.Atoi(proxyPortStr)
	if err != nil || proxyPort <= 0 {
		proxyPort = 8000
		fmt.Printf("Invalid or missing PROXY_PORT. Defaulting to %d.\n", proxyPort)
	}

	deploymentName := os.Getenv("DEPLOYMENT_NAME")
	if deploymentName == "" {
		fmt.Println("DEPLOYMENT_NAME must be set")
		return
	}

	// ------------------------------------------------
	// Read SRO Ingest URL
	// ------------------------------------------------
	sroIngestURL := os.Getenv("SRO_INGEST_URL")
	if sroIngestURL == "" {
		fmt.Println("SRO_INGEST_URL must be set")
		return
	}

	logDir := fmt.Sprintf("/app/logs/%s", deploymentName)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Printf("Failed to create log directory '%s': %v\n", logDir, err)
		return
	}

	// ------------------------------------------------
	//  2) RATE-LIMIT & SLO ENV VARIABLES (WITH DEFAULTS)
	// ------------------------------------------------

	rateLimitStr := os.Getenv("RATE_LIMIT")
	rateLimit, err := strconv.Atoi(rateLimitStr)
	if err != nil || rateLimit <= 0 {
		rateLimit = 350
		fmt.Printf("Invalid or missing RATE_LIMIT. Defaulting to %d.\n", rateLimit)
	} else {
		fmt.Printf("Rate limit set to %d requests per second.\n", rateLimit)
	}

	sloStr := os.Getenv("LATENCY_SLO_MS")
	sloMS, err := strconv.Atoi(sloStr)
	if err != nil || sloMS <= 0 {
		sloMS = 100
		fmt.Printf("Invalid or missing LATENCY_SLO_MS. Defaulting to %dms.\n", sloMS)
	} else {
		fmt.Printf("Median latency SLO set to %d ms.\n", sloMS)
	}

	sloPassThresholdStr := os.Getenv("SLO_PASS_THRESHOLD")
	sloPassThreshold, err := strconv.Atoi(sloPassThresholdStr)
	if err != nil || sloPassThreshold <= 0 {
		sloPassThreshold = 100
		fmt.Println("Invalid or missing SLO_PASS_THRESHOLD. Defaulting to 100.")
	} else {
		fmt.Printf("SLO pass threshold set to %d.\n", sloPassThreshold)
	}

	sloViolationThresholdStr := os.Getenv("SLO_VIOLATION_THRESHOLD")
	sloViolationThreshold, err := strconv.Atoi(sloViolationThresholdStr)
	if err != nil || sloViolationThreshold <= 0 {
		sloViolationThreshold = 100
		fmt.Println("Invalid or missing SLO_VIOLATION_THRESHOLD. Defaulting to 100.")
	} else {
		fmt.Printf("SLO violation threshold set to %d.\n", sloViolationThreshold)
	}

	// ------------------------------------------------
	//  LEAKY-BUCKET QUEUE ENV
	// ------------------------------------------------
	queueSizeStr := os.Getenv("QUEUE_SIZE")
	queueSize, err := strconv.Atoi(queueSizeStr)
	if err != nil || queueSize < 0 {
		queueSize = 1
		fmt.Printf("Invalid or missing QUEUE_SIZE. Defaulting to %d.\n", queueSize)
	} else {
		fmt.Printf("Queue size set to %d.\n", queueSize)
	}

	// ------------------------------------------------
	//  Create a unique log filename for *this* pod
	// ------------------------------------------------
	timestamp := time.Now().Format("2006-01-02T15-04-05")
	newLogFileName := fmt.Sprintf("%s/request_logs_%s_%s.txt", logDir, timestamp, podName)
	logFile, err := os.Create(newLogFileName)
	if err != nil {
		fmt.Printf("Failed to create log file '%s': %v\n", newLogFileName, err)
		return
	}
	defer logFile.Close()

	eventsLogFileName := fmt.Sprintf("%s/event_logs_%s_%s.txt", logDir, timestamp, podName)
	eventsFile, err := os.Create(eventsLogFileName)
	if err != nil {
		fmt.Printf("Failed to create events log file '%s': %v\n", eventsLogFileName, err)
		return
	}
	defer eventsFile.Close()

	fmt.Printf("Logging requests to file: %s\n", newLogFileName)
	fmt.Printf("Logging events to file:   %s\n", eventsLogFileName)

	// ------------------------------------------------
	//  INITIALIZE OUR PROXY OBJECT
	// ------------------------------------------------

	proxy := &Proxy{
		rateLimit:    rateLimit,
		dripInterval: computeDripInterval(rateLimit),
		requestQueue: make(chan struct{}, queueSize),
		queueSize:    queueSize,

		logFile:       logFile,
		logEventsFile: eventsFile,

		vmLatencies:         make([]time.Duration, 0),
		serverlessLatencies: make([]time.Duration, 0),

		slo:                   time.Duration(sloMS) * time.Millisecond,
		sloPassThreshold:      sloPassThreshold,
		sloViolationThreshold: sloViolationThreshold,

		httpClient: &http.Client{
			Timeout: 1 * time.Second,
		},

		vmBackendURL:    vmBackendURL,
		vmBackendPort:   vmPort,
		serverlessURL:   serverlessURL,
		serverlessPort:  serverlessPort,
		proxyListenPort: proxyPort,

		windowSize:                5,
		vmLatenciesWindow:         make([][]time.Duration, 5),
		serverlessLatenciesWindow: make([][]time.Duration, 5),
		windowIndex:               0,

		requestCountsMs: make(map[int64]int64),
		deploymentName:  deploymentName,
		sroIngestURL:    sroIngestURL,
		pushBuffer:      make([]int64, 0, 10000), // Pre-allocate 10,000 items
	}

	go proxy.backgroundTasks()
	mux := http.NewServeMux()
	mux.Handle("/", proxy)
	mux.HandleFunc("/setRateLimit", proxy.handleSetRateLimit)

	fmt.Printf("Proxy server is running on port %d\n", proxy.proxyListenPort)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", proxy.proxyListenPort), mux); err != nil {
		fmt.Printf("Error starting server: %v\n", err)
	}
}

func computeDripInterval(rps int) time.Duration {
	if rps < 1 {
		rps = 1
	}
	fmt.Printf("Rate limit set to %d requests per second.\n", rps)
	fmt.Printf("Drip interval: %v\n", time.Second/time.Duration(rps))
	return time.Second / time.Duration(rps)
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path == "/healthz" || req.URL.Path == "/ready" {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
		return
	}

	// Record arrival time
	arrivalTime := time.Now()

	// Record the arrival at millisecond resolution.
	currentMs := arrivalTime.UnixMilli()
	p.muRequestCounts.Lock()
	p.requestCountsMs[currentMs]++
	p.muRequestCounts.Unlock()

	// Try to join the queue
	select {
	case p.requestQueue <- struct{}{}:
	default:
		atomic.AddInt64(&p.serverlessRequestsThisSecond, 1)
		p.forwardToServerless(w, req, arrivalTime)
		return
	}
	defer func() { <-p.requestQueue }()

	p.leakyWait()

	atomic.AddInt64(&p.vmRequestsThisSecond, 1)
	vmURL := fmt.Sprintf("%s:%d%s", p.vmBackendURL, p.vmBackendPort, req.URL.RequestURI())
	res, duration, err := p.forwardRequest(req, vmURL)

	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			p.bufferStats(req, nil, arrivalTime, 1000*time.Millisecond, "VM Backend")
			http.Error(w, "VM backend timed out", http.StatusGatewayTimeout)
			return
		}
		http.Error(w, "VM backend unavailable", http.StatusServiceUnavailable)
		return
	}

	p.writeResponse(w, res)
	p.recordVMLatency(duration)
	p.bufferStats(req, res, arrivalTime, duration, "VM Backend")
}

func (p *Proxy) leakyWait() {
	p.dripMu.Lock()
	defer p.dripMu.Unlock()

	earliest := p.lastDripTime.Add(p.dripInterval)
	now := time.Now()
	if now.Before(earliest) {
		time.Sleep(earliest.Sub(now))
		now = earliest
	}
	p.lastDripTime = now
}

func (p *Proxy) backgroundTasks() {

	// First, align to the top of the next wall-clock second.
	now := time.Now()
	next := now.Truncate(time.Second).Add(time.Second)
	time.Sleep(time.Until(next))

	// Then, run every second
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {

		previousSecond := time.Now().Add(-1 * time.Second).Truncate(time.Second)
		prevSecMs := previousSecond.UnixMilli()

		p.muRequestCounts.Lock()

		// Extract exactly 1,000 buckets, one per millisecond.
		var totalSecRequests int64 = 0
		for i := int64(0); i < 1000; i++ {
			targetMs := prevSecMs + i
			count := p.requestCountsMs[targetMs]

			if len(p.pushBuffer) == 0 {
				p.pushStartMs = targetMs
			}
			p.pushBuffer = append(p.pushBuffer, count)
			totalSecRequests += count

			// Remove from map to free memory
			delete(p.requestCountsMs, targetMs)
		}

		// Prevent memory leaks from severely delayed requests hitting old keys
		for k := range p.requestCountsMs {
			if k < prevSecMs {
				delete(p.requestCountsMs, k)
			}
		}

		p.muRequestCounts.Unlock()

		// Print the 1-second total to console for verification
		fmt.Printf("ts=%s req=%d\n", previousSecond.Format(time.RFC3339), totalSecRequests)

		// Push after collecting ten seconds.
		if len(p.pushBuffer) >= 10000 {
			payload := HighResPayload{
				DeploymentName:   p.deploymentName,
				StartTimestampMs: p.pushStartMs,
				Data:             append([]int64(nil), p.pushBuffer...),
			}
			p.pushBuffer = p.pushBuffer[:0]

			go p.pushMetricsToSRO(payload)
		}

		// Move latencies for this second to the 5-sec ring buffers
		p.latencyLock.Lock()
		p.vmLatenciesWindow[p.windowIndex] = p.vmLatencies
		p.serverlessLatenciesWindow[p.windowIndex] = p.serverlessLatencies
		p.vmLatencies = make([]time.Duration, 0)
		p.serverlessLatencies = make([]time.Duration, 0)
		p.latencyLock.Unlock()

		p.windowIndex = (p.windowIndex + 1) % p.windowSize

		_, vmP95ThisSecond := p.compute5SecVMStats()

		if vmP95ThisSecond > 0 {
			if vmP95ThisSecond > p.slo {
				p.consecutiveSLOViolations++
				p.consecutiveSLOPasses = 0
				if p.consecutiveSLOViolations >= p.sloViolationThreshold {
					newRate := int(float64(p.rateLimit) * 0.95)
					if newRate < 1 {
						newRate = 1
					}
					fmt.Printf("SLO violated for %d consecutive intervals. Reducing rate limit from %d to %d.\n",
						p.sloViolationThreshold, p.rateLimit, newRate)

					p.logEvent("[%s] SLO violated for %d consecutive intervals. Reducing rate limit from %d to %d.\n",
						time.Now().Format(time.RFC3339Nano),
						p.sloViolationThreshold, p.rateLimit, newRate)

					p.setRateLimit(newRate)
					p.consecutiveSLOViolations = 0
				}
			}
		}

		// Flush logs
		p.logBufMu.Lock()
		for _, entry := range p.logBuffer {
			if _, err := p.logFile.WriteString(entry); err != nil {
				fmt.Printf("Failed writing logs: %v\n", err)
				break
			}
		}
		p.logBuffer = p.logBuffer[:0]
		p.logBufMu.Unlock()

		p.eventsBufMu.Lock()
		for _, entry := range p.eventsLogBuffer {
			if _, err := p.logEventsFile.WriteString(entry); err != nil {
				fmt.Printf("Failed writing event logs: %v\n", err)
				break
			}
		}
		p.eventsLogBuffer = p.eventsLogBuffer[:0]
		p.eventsBufMu.Unlock()
	}
}

func (p *Proxy) pushMetricsToSRO(payload HighResPayload) {
	time.Sleep(time.Duration(rand.Intn(1000)) * time.Millisecond)

	dataBytes, err := json.Marshal(payload)
	if err != nil {
		fmt.Printf("Failed to marshal SRO push payload: %v\n", err)
		return
	}

	req, err := http.NewRequest("POST", p.sroIngestURL, bytes.NewBuffer(dataBytes))
	if err != nil {
		fmt.Printf("Failed to create SRO ingest request: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := p.httpClient.Do(req)
	if err != nil {
		fmt.Printf("Failed to push metrics to SRO: %v\n", err)
		return
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		fmt.Printf("SRO returned status %d on ingest\n", res.StatusCode)
	}
}

func (p *Proxy) compute5SecVMStats() (time.Duration, time.Duration) {
	var allVM []time.Duration
	for i := 0; i < p.windowSize; i++ {
		allVM = append(allVM, p.vmLatenciesWindow[i]...)
	}
	if len(allVM) == 0 {
		return 0, 0
	}

	sort.Slice(allVM, func(i, j int) bool {
		return allVM[i] < allVM[j]
	})
	median := allVM[len(allVM)/2]
	index95 := int(float64(len(allVM)) * 0.95)
	if index95 >= len(allVM) {
		index95 = len(allVM) - 1
	}
	p95 := allVM[index95]
	return median, p95
}

func (p *Proxy) compute5SecServerlessStats() (time.Duration, time.Duration) {
	var allSrv []time.Duration
	for i := 0; i < p.windowSize; i++ {
		allSrv = append(allSrv, p.serverlessLatenciesWindow[i]...)
	}
	if len(allSrv) == 0 {
		return 0, 0
	}

	sort.Slice(allSrv, func(i, j int) bool {
		return allSrv[i] < allSrv[j]
	})
	median := allSrv[len(allSrv)/2]
	index95 := int(float64(len(allSrv)) * 0.95)
	if index95 >= len(allSrv) {
		index95 = len(allSrv) - 1
	}
	p95 := allSrv[index95]
	return median, p95
}

func (p *Proxy) forwardToServerless(w http.ResponseWriter, req *http.Request, arrivalTime time.Time) {
	fullURL := fmt.Sprintf("%s%s", p.serverlessURL, req.URL.RequestURI())
	res, duration, err := p.forwardRequest(req, fullURL)
	if err != nil {
		http.Error(w, "Serverless backend unavailable", http.StatusServiceUnavailable)
		return
	}
	p.writeResponse(w, res)
	p.recordServerlessLatency(duration)

	p.bufferStats(req, res, arrivalTime, duration, "Serverless Backend")
}

func (p *Proxy) forwardRequest(req *http.Request, backendURL string) (*http.Response, time.Duration, error) {
	proxyReq, err := http.NewRequest(req.Method, backendURL, req.Body)
	if err != nil {
		return nil, 0, err
	}
	proxyReq.Header = req.Header.Clone()

	start := time.Now()
	res, err := p.httpClient.Do(proxyReq)
	duration := time.Since(start)
	return res, duration, err
}

func (p *Proxy) writeResponse(w http.ResponseWriter, res *http.Response) {
	for name, values := range res.Header {
		for _, val := range values {
			w.Header().Add(name, val)
		}
	}
	w.Header().Set("Proxy", "hybrid-proxy")
	w.WriteHeader(res.StatusCode)
	io.Copy(w, res.Body)
	res.Body.Close()
}

func (p *Proxy) bufferStats(
	req *http.Request,
	res *http.Response,
	arrivalTime time.Time,
	duration time.Duration,
	backend string,
) {
	statusCode := 0
	if res != nil {
		statusCode = res.StatusCode
	}

	totalDuration := time.Since(arrivalTime)

	logEntry := fmt.Sprintf(
		"[%s] %s - Request Duration: %s,Total Duration: %s, Status Code: %d\n",
		arrivalTime.Format(time.RFC3339Nano),
		backend,
		duration,
		totalDuration,
		statusCode,
	)

	p.logBufMu.Lock()
	defer p.logBufMu.Unlock()
	p.logBuffer = append(p.logBuffer, logEntry)
}

func (p *Proxy) recordVMLatency(duration time.Duration) {
	p.latencyLock.Lock()
	defer p.latencyLock.Unlock()
	p.vmLatencies = append(p.vmLatencies, duration)
}

func (p *Proxy) recordServerlessLatency(duration time.Duration) {
	p.latencyLock.Lock()
	defer p.latencyLock.Unlock()
	p.serverlessLatencies = append(p.serverlessLatencies, duration)
}

func (p *Proxy) setRateLimit(newRateLimit int) {
	if newRateLimit < 1 {
		newRateLimit = 1
	}
	if newRateLimit == p.rateLimit {
		return
	}

	fmt.Printf("Rate limit updated from %d to %d\n", p.rateLimit, newRateLimit)
	p.rateLimit = newRateLimit

	p.dripMu.Lock()
	p.dripInterval = computeDripInterval(newRateLimit)
	p.dripMu.Unlock()
}

func (p *Proxy) handleSetRateLimit(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	if limitStr == "" {
		http.Error(w, "Missing 'limit' query parameter", http.StatusBadRequest)
		return
	}
	newLimit, err := strconv.Atoi(limitStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid 'limit' param: %v", err), http.StatusBadRequest)
		return
	}
	p.setRateLimit(newLimit)

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Rate limit updated to %d\n", p.rateLimit)
}

func (p *Proxy) logEvent(format string, a ...interface{}) {
	entry := fmt.Sprintf(format, a...)
	p.eventsBufMu.Lock()
	defer p.eventsBufMu.Unlock()
	p.eventsLogBuffer = append(p.eventsLogBuffer, entry)
}
