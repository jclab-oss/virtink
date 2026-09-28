package daemon

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	virtv1alpha1 "github.com/smartxworks/virtink/pkg/apis/virt/v1alpha1"
	"github.com/smartxworks/virtink/pkg/cloudhypervisor"
	"github.com/smartxworks/virtink/pkg/daemon/balloon"
	"github.com/smartxworks/virtink/pkg/daemon/pid"
)

// BalloonManager periodically resizes balloons of VMs on this node according to the node memory pressure.
type BalloonManager struct {
	client.Client

	NodeName string
	Config   balloon.Config
	Interval time.Duration
	ProcPath string
}

func (m *BalloonManager) Start(ctx context.Context) error {
	log := ctrl.LoggerFrom(ctx).WithName("balloon-manager")
	ticker := time.NewTicker(m.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := m.adjust(ctrl.LoggerInto(ctx, log)); err != nil {
				log.Error(err, "adjust balloons")
			}
		}
	}
}

func (m *BalloonManager) adjust(ctx context.Context) error {
	log := ctrl.LoggerFrom(ctx)
	node, err := balloon.ReadNodeStats(m.ProcPath)
	if err != nil {
		return fmt.Errorf("read node stats: %s", err)
	}
	pressure := m.Config.Pressure(node)
	if pressure == balloon.PressureHold {
		return nil
	}

	var vmList virtv1alpha1.VirtualMachineList
	if err := m.List(ctx, &vmList); err != nil {
		return fmt.Errorf("list VMs: %s", err)
	}
	for i := range vmList.Items {
		vm := &vmList.Items[i]
		if !vm.Spec.Instance.Memory.IsBallooningEnabled() || vm.Status.NodeName != m.NodeName ||
			vm.Status.Phase != virtv1alpha1.VirtualMachineRunning || vm.Status.Migration != nil {
			continue
		}
		if err := m.adjustVM(ctx, vm, pressure); err != nil {
			log.Error(err, "adjust balloon", "vm", client.ObjectKeyFromObject(vm))
		}
	}
	return nil
}

func (m *BalloonManager) adjustVM(ctx context.Context, vm *virtv1alpha1.VirtualMachine, pressure balloon.Pressure) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	socketPath := filepath.Join(getVMDataDirPath(vm), "ch.sock")
	chClient := cloudhypervisor.NewClient(socketPath)
	vmInfo, err := chClient.VmInfo(ctx)
	if err != nil {
		return fmt.Errorf("get VM info: %s", err)
	}
	if vmInfo.State != "Running" || vmInfo.Config.Balloon == nil {
		return nil
	}

	state := balloon.VMState{
		MinSize:     vm.Spec.Instance.Memory.MinSize.Value(),
		MaxSize:     vmInfo.Config.Memory.Size,
		BalloonSize: vmInfo.Config.Balloon.Size,
	}
	if chPID, err := pid.GetPIDBySocket(socketPath); err == nil {
		if rss, err := balloon.ReadRSS(m.ProcPath, chPID); err == nil {
			state.RSS = rss
		}
	}

	target := m.Config.TargetBalloonSize(pressure, state)
	if target == state.BalloonSize {
		return nil
	}
	if err := chClient.VmResizeBalloon(ctx, target); err != nil {
		return fmt.Errorf("resize balloon: %s", err)
	}
	ctrl.LoggerFrom(ctx).Info("resized balloon", "vm", client.ObjectKeyFromObject(vm), "pressure", pressure,
		"from", state.BalloonSize, "to", target, "rss", state.RSS)
	return nil
}
