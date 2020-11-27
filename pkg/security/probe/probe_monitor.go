// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-2020 Datadog, Inc.

// +build linux

package probe

import (
	"context"

	"github.com/DataDog/datadog-go/statsd"
	"github.com/DataDog/ebpf/manager"
	"github.com/pkg/errors"
)

// Monitor regroups all the work we want to do to monitor the probes we pushed in the kernel
type Monitor struct {
	client *statsd.Client

	loadController *LoadController

	perfBufferMonitor *PerfBufferMonitor
	syscallMonitor    *SyscallMonitor
}

// NewMonitor returns a new instance of a ProbeMonitor
func NewMonitor(p *Probe, client *statsd.Client) (*Monitor, error) {
	var err error
	m := &Monitor{
		client: client,
	}

	// instantiate a new load controller
	m.loadController, err = NewLoadController(p, client)
	if err != nil {
		return nil, err
	}

	// instantiate a new event statistics monitor
	m.perfBufferMonitor, err = NewPerfBufferMonitor(p.manager, p.managerOptions, p.config)
	if err != nil {
		return nil, errors.Wrap(err, "couldn't create the events statistics monitor")
	}

	// create a new syscall monitor if requested
	if p.config.SyscallMonitor {
		m.syscallMonitor, err = NewSyscallMonitor(p.manager)
		if err != nil {
			return nil, err
		}
	}
	return m, nil
}

// GetPerfBufferMonitor returns the perf buffer monitor
func (m *Monitor) GetPerfBufferMonitor() *PerfBufferMonitor {
	return m.perfBufferMonitor
}

// Start triggers the goroutine of all the underlying controllers and monitors of the Monitor
func (m *Monitor) Start(ctx context.Context) error {
	go m.loadController.Start(ctx)
	return nil
}

// SendStats sends statistics about the probe to Datadog
func (m *Monitor) SendStats() error {
	if m.syscallMonitor != nil {
		if err := m.syscallMonitor.SendStats(m.client); err != nil {
			return errors.Wrap(err, "failed to send syscall monitor stats")
		}
	}

	if err := m.perfBufferMonitor.SendStats(m.client); err != nil {
		return errors.Wrap(err, "failed to send events stats")
	}

	return nil
}

// GetStats returns Stats according to the system-probe module format
func (m *Monitor) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	var syscalls *SyscallStats
	var err error

	if m.syscallMonitor != nil {
		syscalls, err = m.syscallMonitor.GetStats()
	}

	stats["events"] = map[string]interface{}{
		"perf_buffer": 0,
		"syscalls":    syscalls,
	}
	return stats, err
}

// ProcessEvent processes an event through the various monitors and controllers of the probe
func (m *Monitor) ProcessEvent(event *Event, size uint64, CPU int, perfMap *manager.PerfMap) {
	m.perfBufferMonitor.CountEvent(event.GetEventType(), 1, size, perfMap, CPU)
	m.loadController.Count(event.GetEventType(), event.Process.Pid)
}
