// Package gpu provides GPU metrics collection using NVIDIA's go-nvml bindings.
package gpu

import (
	"fmt"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

// DeviceMetrics holds GPU metrics for a single device.
type DeviceMetrics struct {
	UUID           string
	Name           string
	Index          int
	GPUUtilization uint32 // 0-100
	MemUtilization uint32 // 0-100
	MemUsedBytes   uint64
	MemTotalBytes  uint64
	TemperatureC   uint32
	PowerWatts     uint32
	PowerLimitW    uint32
	PIDs           []uint32 // Compute processes running on this GPU
}

// Collector gathers GPU metrics via NVML.
type Collector struct{}

// NewCollector creates a new GPU metrics collector.
// NVML must be initialized before calling Collect().
func NewCollector() *Collector {
	return &Collector{}
}

// Init initializes the NVML library. Must be called once before Collect().
func Init() error {
	ret := nvml.Init()
	if ret != nvml.SUCCESS {
		return fmt.Errorf("nvml.Init failed: %v", nvml.ErrorString(ret))
	}
	return nil
}

// Shutdown cleans up the NVML library.
func Shutdown() {
	nvml.Shutdown()
}

// Collect gathers metrics from all GPU devices on this node.
func (c *Collector) Collect() ([]DeviceMetrics, error) {
	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("DeviceGetCount: %v", nvml.ErrorString(ret))
	}

	metrics := make([]DeviceMetrics, 0, count)
	for i := 0; i < count; i++ {
		device, ret := nvml.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			return nil, fmt.Errorf("DeviceGetHandleByIndex(%d): %v", i, nvml.ErrorString(ret))
		}

		m, err := collectDevice(device, i)
		if err != nil {
			return nil, fmt.Errorf("device %d: %w", i, err)
		}
		metrics = append(metrics, m)
	}
	return metrics, nil
}

func collectDevice(device nvml.Device, index int) (DeviceMetrics, error) {
	m := DeviceMetrics{Index: index}

	uuid, ret := device.GetUUID()
	if ret != nvml.SUCCESS {
		return m, fmt.Errorf("GetUUID: %v", nvml.ErrorString(ret))
	}
	m.UUID = uuid

	name, ret := device.GetName()
	if ret != nvml.SUCCESS {
		return m, fmt.Errorf("GetName: %v", nvml.ErrorString(ret))
	}
	m.Name = name

	util, ret := device.GetUtilizationRates()
	if ret == nvml.SUCCESS {
		m.GPUUtilization = uint32(util.Gpu)
		m.MemUtilization = uint32(util.Memory)
	}

	memInfo, ret := device.GetMemoryInfo()
	if ret == nvml.SUCCESS {
		m.MemUsedBytes = memInfo.Used
		m.MemTotalBytes = memInfo.Total
	}

	temp, ret := device.GetTemperature(nvml.TEMPERATURE_GPU)
	if ret == nvml.SUCCESS {
		m.TemperatureC = uint32(temp)
	}

	power, ret := device.GetPowerUsage()
	if ret == nvml.SUCCESS {
		m.PowerWatts = uint32(power / 1000) // milliwatts to watts
	}

	powerLimit, ret := device.GetPowerManagementLimit()
	if ret == nvml.SUCCESS {
		m.PowerLimitW = uint32(powerLimit / 1000)
	}

	// Get PIDs of compute processes on this GPU
	procs, ret := device.GetComputeRunningProcesses()
	if ret == nvml.SUCCESS {
		for _, p := range procs {
			m.PIDs = append(m.PIDs, uint32(p.Pid))
		}
	}

	return m, nil
}
