package metrics

import (
	"context"
	"os"
	"runtime"
	"time"

	"github.com/FastLane-Labs/fastlane-sidecar/pkg/log"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

// SystemMetricsCollector collects system-level metrics
type SystemMetricsCollector struct {
	metrics *Metrics
	ctx     context.Context
	cancel  context.CancelFunc
	proc    *process.Process

	// Last values for calculating deltas
	lastDiskRead    uint64
	lastDiskWrite   uint64
	lastNetworkRecv uint64
	lastNetworkSent uint64
}

// NewSystemMetricsCollector creates a new system metrics collector
func NewSystemMetricsCollector(metrics *Metrics) *SystemMetricsCollector {
	ctx, cancel := context.WithCancel(context.Background())

	// Get current process
	proc, err := process.NewProcess(int32(os.Getpid()))
	if err != nil {
		log.Error("Failed to get process info", "error", err)
	}

	return &SystemMetricsCollector{
		metrics: metrics,
		ctx:     ctx,
		cancel:  cancel,
		proc:    proc,
	}
}

// Start begins collecting system metrics
func (c *SystemMetricsCollector) Start() {
	// Initialize baseline values
	c.updateDiskIO()
	c.updateNetworkIO()

	go c.collectLoop()
}

// Stop stops collecting system metrics
func (c *SystemMetricsCollector) Stop() {
	c.cancel()
}

// collectLoop periodically collects system metrics
func (c *SystemMetricsCollector) collectLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.collectMetrics()
		}
	}
}

// collectMetrics collects all system metrics
func (c *SystemMetricsCollector) collectMetrics() {
	c.updateCPU()
	c.updateMemory()
	c.updateDiskIO()
	c.updateNetworkIO()
	c.updateGoroutines()
}

// updateGoroutines updates goroutine count
func (c *SystemMetricsCollector) updateGoroutines() {
	c.metrics.GoroutinesCount.Store(uint64(runtime.NumGoroutine()))
}

// updateCPU updates CPU usage metrics
func (c *SystemMetricsCollector) updateCPU() {
	if c.proc == nil {
		return
	}

	// Get CPU percent for this process
	cpuPercent, err := c.proc.Percent(0)
	if err != nil {
		log.Debug("Failed to get CPU percent", "error", err)
		return
	}

	c.metrics.SetCPUUsagePercent(cpuPercent)
}

// updateMemory updates memory usage metrics
func (c *SystemMetricsCollector) updateMemory() {
	// Get system memory stats
	vmem, err := mem.VirtualMemory()
	if err != nil {
		log.Debug("Failed to get memory stats", "error", err)
		return
	}

	// Get process memory info
	if c.proc != nil {
		memInfo, err := c.proc.MemoryInfo()
		if err == nil {
			c.metrics.MemoryUsageBytes.Store(memInfo.RSS)

			// Calculate percentage of system memory
			if vmem.Total > 0 {
				percentUsed := (float64(memInfo.RSS) / float64(vmem.Total)) * 100
				c.metrics.SetMemoryUsagePercent(percentUsed)
			}
		}
	}
}

// updateDiskIO updates disk I/O metrics
func (c *SystemMetricsCollector) updateDiskIO() {
	if c.proc == nil {
		return
	}

	// Get process I/O counters
	ioCounters, err := c.proc.IOCounters()
	if err != nil {
		log.Debug("Failed to get disk I/O stats", "error", err)
		return
	}

	// Calculate delta since last measurement
	if c.lastDiskRead > 0 {
		readDelta := ioCounters.ReadBytes - c.lastDiskRead
		c.metrics.DiskReadBytes.Add(readDelta)
	}
	if c.lastDiskWrite > 0 {
		writeDelta := ioCounters.WriteBytes - c.lastDiskWrite
		c.metrics.DiskWriteBytes.Add(writeDelta)
	}

	// Update last values
	c.lastDiskRead = ioCounters.ReadBytes
	c.lastDiskWrite = ioCounters.WriteBytes
}

// updateNetworkIO updates network I/O metrics
func (c *SystemMetricsCollector) updateNetworkIO() {
	// Get network I/O counters
	ioCounters, err := net.IOCounters(false) // false = aggregate all interfaces
	if err != nil {
		log.Debug("Failed to get network I/O stats", "error", err)
		return
	}

	if len(ioCounters) == 0 {
		return
	}

	// Use the first (aggregated) counter
	counter := ioCounters[0]

	// Calculate delta since last measurement
	if c.lastNetworkRecv > 0 {
		recvDelta := counter.BytesRecv - c.lastNetworkRecv
		c.metrics.NetworkRecvBytes.Add(recvDelta)
	}
	if c.lastNetworkSent > 0 {
		sentDelta := counter.BytesSent - c.lastNetworkSent
		c.metrics.NetworkSentBytes.Add(sentDelta)
	}

	// Update last values
	c.lastNetworkRecv = counter.BytesRecv
	c.lastNetworkSent = counter.BytesSent
}

// Helper functions for one-time system info
func GetSystemInfo() map[string]interface{} {
	info := make(map[string]interface{})

	// CPU info
	if cpuInfo, err := cpu.Info(); err == nil && len(cpuInfo) > 0 {
		info["cpu_model"] = cpuInfo[0].ModelName
		info["cpu_cores"] = cpuInfo[0].Cores
	}

	// Memory info
	if vmem, err := mem.VirtualMemory(); err == nil {
		info["memory_total_bytes"] = vmem.Total
	}

	// Disk info
	if diskStat, err := disk.Usage("/"); err == nil {
		info["disk_total_bytes"] = diskStat.Total
		info["disk_free_bytes"] = diskStat.Free
	}

	return info
}
