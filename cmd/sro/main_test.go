package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func resetIngestState(deployment string) {
	cfg = Config{DeploymentName: deployment}
	metricsMutex.Lock()
	metricsMap = make(map[int64]int64)
	metricsMutex.Unlock()
}

func TestIngestHighResMetricsAggregatesPayloads(t *testing.T) {
	resetIngestState("ratings")
	body := `{"deploymentName":"ratings","startTimestampMs":1000,"data":[1,0,2]}`

	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "/metrics/ingest", strings.NewReader(body))
		response := httptest.NewRecorder()
		ingestHighResMetrics(response, req)
		if response.Code != http.StatusOK {
			t.Fatalf("unexpected status: got %d, want %d", response.Code, http.StatusOK)
		}
	}

	metricsMutex.Lock()
	defer metricsMutex.Unlock()
	if got := metricsMap[1000]; got != 2 {
		t.Errorf("bucket 1000: got %d, want 2", got)
	}
	if got := metricsMap[1002]; got != 4 {
		t.Errorf("bucket 1002: got %d, want 4", got)
	}
}

func TestIngestHighResMetricsRejectsDifferentDeployment(t *testing.T) {
	resetIngestState("ratings")
	req := httptest.NewRequest(http.MethodPost, "/metrics/ingest", strings.NewReader(
		`{"deploymentName":"details","startTimestampMs":1000,"data":[1]}`,
	))
	response := httptest.NewRecorder()

	ingestHighResMetrics(response, req)

	if response.Code != http.StatusConflict {
		t.Fatalf("unexpected status: got %d, want %d", response.Code, http.StatusConflict)
	}
}

func TestSimulateExactQueueWithoutPodsOffloadsEverything(t *testing.T) {
	buffer := make([]int64, 1000)
	buffer[0] = 3
	buffer[999] = 2

	result := simulateExactQueue1ms(buffer, 0, 9, 1000, 10)
	if len(result) != 1 || result[0].Rps != 5 {
		t.Fatalf("unexpected offload result: %+v", result)
	}
}

func TestSimulateExactQueueCapturesMillisecondOverflow(t *testing.T) {
	buffer := make([]int64, 1000)
	buffer[0] = 20

	result := simulateExactQueue1ms(buffer, 1, 9, 1000, 10)
	if len(result) != 1 || result[0].Rps != 11 {
		t.Fatalf("unexpected offload result: %+v", result)
	}
}
