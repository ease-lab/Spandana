// costcalc.go

package costcalc

import (
	"fmt"
	"log"
	"time"
)

// LoadPoint is a small struct to track RPS and timestamp
type LoadPoint struct {
	Timestamp time.Time
	Rps       int64
}

// ---------------------------------------------------------------------
// 1) Shared cost functions: m5.large cost and Lambda cost
// ---------------------------------------------------------------------

// CalculatePodCost mirrors the Python function for m5.large
// We treat "totalMilicoreSeconds" as how many milicores we consumed over time.
func CalculatePodCost(totalMilicoreSeconds float64) float64 {
	const costPerHourPerVM = 0.086
	const totalMilicoresPerVM = 2000.0
	const secondsPerHour = 3600.0

	totalMilicoreSecondsPerHour := totalMilicoresPerVM * secondsPerHour
	costPerMilicoreSecond := costPerHourPerVM / totalMilicoreSecondsPerHour

	return totalMilicoreSeconds * costPerMilicoreSecond
}

func CalculateVMCost(numberOfReplicas int64, totalSeconds float64) float64 {
	const costPerVMPerHour = 0.086
	vmCostPerSecond := costPerVMPerHour / 3600.0

	const numberOfReplicasPerVM = 2
	vmsRequired := numberOfReplicas / numberOfReplicasPerVM

	log.Printf("vmsRequired: %d", vmsRequired)
	log.Printf("totalSeconds: %f", totalSeconds)
	totalVMSeconds := float64(vmsRequired) * totalSeconds
	vmCost := totalVMSeconds * vmCostPerSecond

	log.Printf("VM cost: %f", vmCost)

	return vmCost
}

// CalculateLambdaCost mirrors the standard AWS Lambda pricing
func CalculateLambdaCost(numInvocations int64, totalGBSeconds float64) float64 {
	// US East (N. Virginia) cost
	const pricePer1MRequests = 0.20
	const pricePerGBSecond = 0.0000166667

	requestCost := (float64(numInvocations) / 1_000_000.0) * pricePer1MRequests
	computeCost := totalGBSeconds * pricePerGBSecond
	return requestCost + computeCost
}

// ---------------------------------------------------------------------
// 2) Vertical-Scaling Logic: "ComputeVMAndLambdaCost"
// ---------------------------------------------------------------------
//
// This is your original vertical-scaling cost calculation, where
// "threshold" is an RPS threshold. Up to that threshold are served by a
// single VM, whose CPU usage scales up/down. The rest goes to Lambda.
func ComputeVMAndLambdaCost(
	loadPattern []LoadPoint,
	threshold int64,
	rpsPerVCPU float64,
	execTimeMs float64,
	memoryMB float64,
) (float64, float64) {

	memoryGB := memoryMB / 1024.0
	execTimeSec := execTimeMs / 1000.0

	var totalVMMilicoreSeconds float64
	var totalLambdaInvocations int64
	var totalLambdaGBSeconds float64

	// For each second in the load pattern:
	for _, lp := range loadPattern {
		// VM handles up to 'threshold' RPS, remainder goes to Lambda
		vmServedRPS := threshold
		lamRPS := lp.Rps - vmServedRPS
		if lamRPS < 0 {
			lamRPS = 0
		}

		// Convert VM-served RPS -> used milicores
		// If 1 vCPU (1000m) can handle rpsPerVCPU RPS, then
		// vmServedRPS RPS needs (vmServedRPS / rpsPerVCPU)*1000 milicores.
		vmMilicores := (float64(vmServedRPS) / rpsPerVCPU) * 1000.0
		// Each LoadPoint is presumably 1 second:
		totalVMMilicoreSeconds += vmMilicores

		// Lambda usage
		totalLambdaInvocations += lamRPS
		totalLambdaGBSeconds += float64(lamRPS) * execTimeSec * memoryGB
	}

	// Now compute final costs
	vmCost := CalculatePodCost(totalVMMilicoreSeconds)
	lambdaCost := CalculateLambdaCost(totalLambdaInvocations, totalLambdaGBSeconds)
	return vmCost, lambdaCost
}

// ---------------------------------------------------------------------
// 3) Horizontal-Scaling Logic: "ComputePodAndLambdaCost"
// ---------------------------------------------------------------------
//
// This new function uses horizontal scaling: 'threshold' is the number of Pods.
// Each Pod has a fixed CPU size in milicores (podSizeMilicores) and a
// fixed RPS capacity (podRPSCapacity).
//
// If the total RPS at a given time > threshold * podRPSCapacity, overflow
// goes to Lambda. The Pod cost is a flat cost of "threshold" pods running
// for every second in the pattern.
func ComputePodAndLambdaCost(
	loadPattern []LoadPoint,
	threshold int64, // number of Pods
	podSizeMilicores int64, // e.g. 500m
	podRPSCapacity int64, // e.g. 300 RPS
	execTimeMs float64,
	memoryMB float64,
) (float64, float64) {

	memoryGB := memoryMB / 1024.0
	execTimeSec := execTimeMs / 1000.0

	var totalLambdaInvocations int64
	var totalLambdaGBSeconds float64

	// total time intervals in loadPattern, each presumably 1 second
	numSeconds := float64(len(loadPattern))

	// If you have 'threshold' pods, each with 'podSizeMilicores', you pay for
	// that total CPU over all timestamps in the loadPattern:
	totalPodMilicoreSeconds := float64(threshold*podSizeMilicores) * numSeconds

	for _, lp := range loadPattern {
		// total capacity from all pods combined:
		podsCapacityRPS := threshold * podRPSCapacity
		leftoverRPS := lp.Rps - podsCapacityRPS
		if leftoverRPS < 0 {
			leftoverRPS = 0
		}
		// those leftover requests to Lambda
		totalLambdaInvocations += leftoverRPS
		totalLambdaGBSeconds += float64(leftoverRPS) * execTimeSec * memoryGB
	}

	// Now compute final costs
	podCost := CalculatePodCost(totalPodMilicoreSeconds)
	// podCost := CalculateVMCost(threshold, numSeconds)
	lambdaCost := CalculateLambdaCost(totalLambdaInvocations, totalLambdaGBSeconds)

	// Print out the costs
	fmt.Printf("# Pods/RPS threshold %d/%d, Pod cost=%.9f, Lambda cost=%.9f, Total cost=%.9f (horizontal scaling)\n", threshold, threshold*podRPSCapacity, podCost, lambdaCost, podCost+lambdaCost)
	return podCost, lambdaCost
}
