package volumeutil

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	cdiv1beta1 "kubevirt.io/containerized-data-importer-api/pkg/apis/core/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	virtv1alpha1 "github.com/smartxworks/virtink/pkg/apis/virt/v1alpha1"
)

func TestIsWaitingForFirstConsumer(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := cdiv1beta1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	newDataVolume := func(name string, phase cdiv1beta1.DataVolumePhase) *cdiv1beta1.DataVolume {
		return &cdiv1beta1.DataVolume{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Status:     cdiv1beta1.DataVolumeStatus{Phase: phase},
		}
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		newDataVolume("wffc", cdiv1beta1.WaitForFirstConsumer),
		newDataVolume("importing", cdiv1beta1.ImportInProgress),
		newDataVolume("pending", cdiv1beta1.Pending),
	).Build()

	dataVolume := func(name string) virtv1alpha1.Volume {
		return virtv1alpha1.Volume{VolumeSource: virtv1alpha1.VolumeSource{DataVolume: &virtv1alpha1.DataVolumeVolumeSource{VolumeName: name}}}
	}
	for _, tc := range []struct {
		name   string
		volume virtv1alpha1.Volume
		want   bool
	}{
		{"waiting for first consumer", dataVolume("wffc"), true},
		{"importing", dataVolume("importing"), false},
		{"pending", dataVolume("pending"), false},
		{"not created yet", dataVolume("missing"), false},
		{"not a DataVolume", virtv1alpha1.Volume{VolumeSource: virtv1alpha1.VolumeSource{PersistentVolumeClaim: &virtv1alpha1.PersistentVolumeClaimVolumeSource{}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := IsWaitingForFirstConsumer(context.Background(), c, "default", tc.volume)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
