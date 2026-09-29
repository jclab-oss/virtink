package console

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/rest"

	subresourcesv1alpha1 "github.com/smartxworks/virtink/pkg/apis/subresources/v1alpha1"
)

// VirtualMachineREST is the storage of virtualmachines, which only exists
// since the apiserver requires every subresource, e.g. virtualmachines/console,
// to have a parent storage in its API group, for the parent's scope and kind.
// It supports no verbs, so it serves nothing itself; VMs are served by the
// virt.virtink.smartx.com CRDs.
type VirtualMachineREST struct{}

var _ rest.Storage = &VirtualMachineREST{}
var _ rest.Scoper = &VirtualMachineREST{}
var _ rest.SingularNameProvider = &VirtualMachineREST{}

// New implements rest.Storage. Only kinds of this API group can be returned,
// and VirtualMachineConsoleOptions is the only one.
func (r *VirtualMachineREST) New() runtime.Object {
	return &subresourcesv1alpha1.VirtualMachineConsoleOptions{}
}

// Destroy implements rest.Storage
func (r *VirtualMachineREST) Destroy() {}

// NamespaceScoped implements rest.Scoper
func (r *VirtualMachineREST) NamespaceScoped() bool {
	return true
}

// GetSingularName implements rest.SingularNameProvider
func (r *VirtualMachineREST) GetSingularName() string {
	return "virtualmachine"
}
