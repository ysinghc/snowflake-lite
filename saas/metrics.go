package saas

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// NodeHealth describes a worker pod in the Kubernetes cluster topology.
type NodeHealth struct {
	PodName        string    `json:"podName"`
	DatacenterID   uint64    `json:"datacenterId"`
	MachineID      uint64    `json:"machineId"`
	Status         string    `json:"status"` // "healthy", "rebalancing", "degraded"
	CPUUsagePct    float64   `json:"cpuUsagePct"`
	MemoryUsageMB  float64   `json:"memoryUsageMb"`
	ActiveRequests int64     `json:"activeRequests"`
	TotalIDsMinted uint64    `json:"totalIdsMinted"`
	LastHeartbeat  time.Time `json:"lastHeartbeat"`
}

// SystemMetricsSnapshot represents the aggregated system overview.
type SystemMetricsSnapshot struct {
	TotalIDsGenerated uint64                `json:"totalIdsGenerated"`
	CurrentRPS        uint64                `json:"currentRps"`
	P50LatencyUs      float64               `json:"p50LatencyUs"`
	P95LatencyUs      float64               `json:"p95LatencyUs"`
	P99LatencyUs      float64               `json:"p99LatencyUs"`
	ActiveNodes       int                   `json:"activeNodes"`
	FormatBreakdown   map[string]uint64     `json:"formatBreakdown"`
	ClusterNodes      []NodeHealth          `json:"clusterNodes"`
	BlockedIPCount    int                   `json:"blockedIpCount"`
	UptimeSeconds     int64                 `json:"uptimeSeconds"`
}

// MetricsCollector tracks metrics across the distributed SaaS instance.
type MetricsCollector struct {
	totalIDs atomic.Uint64
	startTime time.Time

	mu            sync.RWMutex
	secondCounts  [60]uint64
	currentSec    int64
	latenciesUs   []float64
	maxLatencies  int
	formatCounts  map[string]uint64
	clusterNodes  map[string]*NodeHealth
}

// NewMetricsCollector initializes system telemetry collector.
func NewMetricsCollector(localPodName string, localDC, localMachine uint64) *MetricsCollector {
	mc := &MetricsCollector{
		startTime:    time.Now().UTC(),
		latenciesUs:  make([]float64, 0, 5000),
		maxLatencies: 5000,
		formatCounts: make(map[string]uint64),
		clusterNodes: make(map[string]*NodeHealth),
	}

	// Register local node + 2 simulated cluster worker nodes for HA overview
	mc.clusterNodes[localPodName] = &NodeHealth{
		PodName:        localPodName,
		DatacenterID:   localDC,
		MachineID:      localMachine,
		Status:         "healthy",
		CPUUsagePct:    12.4,
		MemoryUsageMB:  42.5,
		ActiveRequests: 1,
		LastHeartbeat:  time.Now().UTC(),
	}

	mc.clusterNodes["snowflake-worker-1"] = &NodeHealth{
		PodName:        "snowflake-worker-1",
		DatacenterID:   0,
		MachineID:      1,
		Status:         "healthy",
		CPUUsagePct:    18.1,
		MemoryUsageMB:  48.2,
		ActiveRequests: 4,
		TotalIDsMinted: 1542000,
		LastHeartbeat:  time.Now().UTC(),
	}

	mc.clusterNodes["snowflake-worker-2"] = &NodeHealth{
		PodName:        "snowflake-worker-2",
		DatacenterID:   0,
		MachineID:      2,
		Status:         "healthy",
		CPUUsagePct:    14.7,
		MemoryUsageMB:  45.0,
		ActiveRequests: 2,
		TotalIDsMinted: 1893000,
		LastHeartbeat:  time.Now().UTC(),
	}

	return mc
}

// RecordGeneration logs an ID generation event with latency measurement.
func (mc *MetricsCollector) RecordGeneration(format string, count uint64, elapsed time.Duration) {
	mc.totalIDs.Add(count)

	mc.mu.Lock()
	defer mc.mu.Unlock()

	nowSec := time.Now().Unix()
	slot := nowSec % 60
	if nowSec != mc.currentSec {
		mc.secondCounts[slot] = 0
		mc.currentSec = nowSec
	}
	mc.secondCounts[slot] += count

	mc.formatCounts[format] += count

	latUs := float64(elapsed.Nanoseconds()) / 1000.0
	if len(mc.latenciesUs) >= mc.maxLatencies {
		mc.latenciesUs = mc.latenciesUs[1:]
	}
	mc.latenciesUs = append(mc.latenciesUs, latUs)
}

// GetSnapshot produces full metrics state for Admin overview.
func (mc *MetricsCollector) GetSnapshot(blockedIPs int) SystemMetricsSnapshot {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	nowSec := time.Now().Unix()
	slot := nowSec % 60
	prevSlot := (nowSec - 1) % 60
	if prevSlot < 0 {
		prevSlot += 60
	}

	currentRPS := mc.secondCounts[slot] + mc.secondCounts[prevSlot]

	// Compute latency percentiles
	var p50, p95, p99 float64
	if len(mc.latenciesUs) > 0 {
		sorted := make([]float64, len(mc.latenciesUs))
		copy(sorted, mc.latenciesUs)
		sort.Float64s(sorted)

		p50 = sorted[int(float64(len(sorted))*0.50)]
		p95 = sorted[int(float64(len(sorted))*0.95)]
		p99 = sorted[int(float64(len(sorted))*0.99)]
	} else {
		p50, p95, p99 = 1.2, 4.8, 12.5 // Baseline microsecond defaults
	}

	nodes := make([]NodeHealth, 0, len(mc.clusterNodes))
	for _, n := range mc.clusterNodes {
		nodes = append(nodes, *n)
	}

	fmtMap := make(map[string]uint64)
	for k, v := range mc.formatCounts {
		fmtMap[k] = v
	}

	return SystemMetricsSnapshot{
		TotalIDsGenerated: mc.totalIDs.Load(),
		CurrentRPS:        currentRPS,
		P50LatencyUs:      p50,
		P95LatencyUs:      p95,
		P99LatencyUs:      p99,
		ActiveNodes:       len(nodes),
		FormatBreakdown:   fmtMap,
		ClusterNodes:      nodes,
		BlockedIPCount:    blockedIPs,
		UptimeSeconds:     time.Now().Unix() - mc.startTime.Unix(),
	}
}

// RebalanceCluster simulates dynamic pod rebalancing across machine IDs.
func (mc *MetricsCollector) RebalanceCluster() []NodeHealth {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	for podName, node := range mc.clusterNodes {
		node.Status = "rebalancing"
		node.CPUUsagePct += 5.0
		mc.clusterNodes[podName] = node
	}

	// Schedule status reset back to healthy
	go func() {
		time.Sleep(1500 * time.Millisecond)
		mc.mu.Lock()
		defer mc.mu.Unlock()
		for podName, node := range mc.clusterNodes {
			node.Status = "healthy"
			node.CPUUsagePct -= 5.0
			mc.clusterNodes[podName] = node
		}
	}()

	res := make([]NodeHealth, 0, len(mc.clusterNodes))
	for _, n := range mc.clusterNodes {
		res = append(res, *n)
	}
	return res
}
