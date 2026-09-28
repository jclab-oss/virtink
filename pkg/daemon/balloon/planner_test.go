package balloon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

const gib = 1 << 30

func TestPressure(t *testing.T) {
	c := DefaultConfig()
	tests := []struct {
		node     NodeStats
		pressure Pressure
	}{
		{node: NodeStats{MemTotal: 100 * gib, MemAvailable: 10 * gib}, pressure: PressureHigh},
		{node: NodeStats{MemTotal: 100 * gib, MemAvailable: 25 * gib}, pressure: PressureHold},
		{node: NodeStats{MemTotal: 100 * gib, MemAvailable: 50 * gib}, pressure: PressureNone},
		{node: NodeStats{MemTotal: 100 * gib, MemAvailable: 50 * gib, PSISomeAvg10: 20}, pressure: PressureHigh},
		{node: NodeStats{}, pressure: PressureHold},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.pressure, c.Pressure(tc.node), "%+v", tc.node)
	}
}

func TestTargetBalloonSize(t *testing.T) {
	c := DefaultConfig()
	// step = 5% of (8Gi - 2Gi) = 307.2Mi
	vm := VMState{MinSize: 2 * gib, MaxSize: 8 * gib}
	step := int64(float64(vm.MaxSize-vm.MinSize) * 0.05)
	mibAligned := func(v int64) int64 { return v / mib * mib }

	tests := []struct {
		name     string
		pressure Pressure
		vm       VMState
		balloon  int64
	}{{
		name:     "hold keeps the balloon",
		pressure: PressureHold,
		vm:       with(vm, 1*gib, 3*gib),
		balloon:  1 * gib,
	}, {
		name:     "pressure caps the guest to RSS and steps below it",
		pressure: PressureHigh,
		vm:       with(vm, 0, 5*gib),
		balloon:  mibAligned(3*gib + step),
	}, {
		name:     "pressure without RSS steps from the current size",
		pressure: PressureHigh,
		vm:       with(vm, 0, 0),
		balloon:  mibAligned(step),
	}, {
		name:     "pressure does not go below minSize",
		pressure: PressureHigh,
		vm:       with(vm, 0, 2*gib),
		balloon:  6 * gib,
	}, {
		name:     "RSS above the current size is ignored",
		pressure: PressureHigh,
		vm:       with(vm, 4*gib, 7*gib),
		balloon:  mibAligned(4*gib + step),
	}, {
		name:     "relief deflates by a step",
		pressure: PressureNone,
		vm:       with(vm, 4*gib, 3*gib),
		balloon:  mibAligned(4*gib - step),
	}, {
		name:     "relief does not go beyond maxSize",
		pressure: PressureNone,
		vm:       with(vm, 100*mib, 3*gib),
		balloon:  0,
	}, {
		name:     "minimum step applies to a narrow range",
		pressure: PressureHigh,
		vm:       VMState{MinSize: 1 * gib, MaxSize: 2 * gib},
		balloon:  c.MinStepBytes,
	}, {
		name:     "minSize equal to maxSize never inflates",
		pressure: PressureHigh,
		vm:       VMState{MinSize: 2 * gib, MaxSize: 2 * gib, RSS: 1 * gib},
		balloon:  0,
	}}
	for _, tc := range tests {
		assert.Equal(t, tc.balloon, c.TargetBalloonSize(tc.pressure, tc.vm), tc.name)
	}
}

func with(vm VMState, balloon int64, rss int64) VMState {
	vm.BalloonSize = balloon
	vm.RSS = rss
	return vm
}

func TestReadNodeStats(t *testing.T) {
	procPath := t.TempDir()
	assert.NoError(t, os.WriteFile(filepath.Join(procPath, "meminfo"), []byte("MemTotal:       16384000 kB\nMemFree:         1000000 kB\nMemAvailable:    4096000 kB\n"), 0644))

	stats, err := ReadNodeStats(procPath)
	assert.NoError(t, err)
	assert.Equal(t, NodeStats{MemTotal: 16384000 * 1024, MemAvailable: 4096000 * 1024}, stats)

	assert.NoError(t, os.MkdirAll(filepath.Join(procPath, "pressure"), 0755))
	assert.NoError(t, os.WriteFile(filepath.Join(procPath, "pressure", "memory"), []byte("some avg10=12.50 avg60=3.00 avg300=1.00 total=123\nfull avg10=1.00 avg60=0.00 avg300=0.00 total=10\n"), 0644))

	stats, err = ReadNodeStats(procPath)
	assert.NoError(t, err)
	assert.Equal(t, 12.5, stats.PSISomeAvg10)
}

func TestReadRSS(t *testing.T) {
	procPath := t.TempDir()
	assert.NoError(t, os.MkdirAll(filepath.Join(procPath, "42"), 0755))
	assert.NoError(t, os.WriteFile(filepath.Join(procPath, "42", "status"), []byte("Name:\tcloud-hypervisor\nVmRSS:\t  2097152 kB\n"), 0644))

	rss, err := ReadRSS(procPath, 42)
	assert.NoError(t, err)
	assert.Equal(t, int64(2*gib), rss)

	_, err = ReadRSS(procPath, 43)
	assert.Error(t, err)
}
