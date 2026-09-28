// Package balloon decides balloon sizes of VMs on a node from the node memory pressure.
//
// Memory freed by the guest is returned to the host by free page reporting, so the balloon is only
// inflated when the node is short of memory, and deflated again when the pressure is gone.
package balloon

const mib = 1 << 20

type Config struct {
	// The node is under pressure when MemAvailable/MemTotal drops below LowWatermark, and relieved
	// when it rises above HighWatermark. Balloons are left untouched in between.
	LowWatermark  float64
	HighWatermark float64
	// The node is also under pressure when the "some avg10" memory PSI exceeds PSIThreshold (%).
	PSIThreshold float64
	// StepRatio of (maxSize - minSize) is moved per tick, but not less than MinStepBytes.
	StepRatio    float64
	MinStepBytes int64
}

func DefaultConfig() Config {
	return Config{
		LowWatermark:  0.2,
		HighWatermark: 0.3,
		PSIThreshold:  10,
		StepRatio:     0.05,
		MinStepBytes:  64 * mib,
	}
}

type NodeStats struct {
	MemTotal     int64
	MemAvailable int64
	PSISomeAvg10 float64
}

type Pressure int

const (
	PressureNone Pressure = iota
	PressureHold
	PressureHigh
)

func (c Config) Pressure(node NodeStats) Pressure {
	if node.MemTotal <= 0 {
		return PressureHold
	}
	available := float64(node.MemAvailable) / float64(node.MemTotal)
	switch {
	case available < c.LowWatermark || node.PSISomeAvg10 > c.PSIThreshold:
		return PressureHigh
	case available > c.HighWatermark:
		return PressureNone
	default:
		return PressureHold
	}
}

type VMState struct {
	MinSize int64
	MaxSize int64
	// BalloonSize is the currently requested balloon size.
	BalloonSize int64
	// RSS is the resident memory of the hypervisor process, or 0 if unknown.
	RSS int64
}

// TargetBalloonSize returns the balloon size the VM should be resized to.
func (c Config) TargetBalloonSize(pressure Pressure, vm VMState) int64 {
	current := vm.MaxSize - vm.BalloonSize
	step := int64(float64(vm.MaxSize-vm.MinSize) * c.StepRatio)
	if step < c.MinStepBytes {
		step = c.MinStepBytes
	}

	switch pressure {
	case PressureHigh:
		// With free page reporting, RSS approximates the memory the guest actually holds. Capping
		// the guest to it reclaims nothing by itself, so step below it to make the guest give back.
		if vm.RSS > 0 && vm.RSS < current {
			current = vm.RSS
		}
		current -= step
	case PressureNone:
		current += step
	default:
		return vm.BalloonSize
	}

	if current < vm.MinSize {
		current = vm.MinSize
	}
	if current > vm.MaxSize {
		current = vm.MaxSize
	}
	// Balloon is resized in pages, keep it MiB aligned.
	return (vm.MaxSize - current) / mib * mib
}
