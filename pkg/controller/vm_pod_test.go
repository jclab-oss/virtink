package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	virtv1alpha1 "github.com/smartxworks/virtink/pkg/apis/virt/v1alpha1"
)

func findVolume(pod *corev1.Pod, name string) *corev1.Volume {
	for i := range pod.Spec.Volumes {
		if pod.Spec.Volumes[i].Name == name {
			return &pod.Spec.Volumes[i]
		}
	}
	return nil
}

func findVolumeMount(container *corev1.Container, name string) *corev1.VolumeMount {
	for i := range container.VolumeMounts {
		if container.VolumeMounts[i].Name == name {
			return &container.VolumeMounts[i]
		}
	}
	return nil
}

func TestBuildVMPodWithImageVolumes(t *testing.T) {
	vm := &virtv1alpha1.VirtualMachine{
		Spec: virtv1alpha1.VirtualMachineSpec{
			Instance: virtv1alpha1.Instance{
				Kernel: &virtv1alpha1.Kernel{
					Image:           "kernel",
					ImagePullPolicy: corev1.PullIfNotPresent,
				},
			},
			Volumes: []virtv1alpha1.Volume{{
				Name: "ubuntu",
				VolumeSource: virtv1alpha1.VolumeSource{
					ContainerDisk: &virtv1alpha1.ContainerDiskVolumeSource{
						Image:           "container-disk",
						ImagePullPolicy: corev1.PullAlways,
					},
				},
			}},
		},
	}

	r := &VMReconciler{PrerunnerImageName: "prerunner"}
	pod, err := r.buildVMPod(context.Background(), vm)
	require.NoError(t, err)
	vmContainer := &pod.Spec.Containers[0]

	kernelVolume := findVolume(pod, "virtink-kernel")
	require.NotNil(t, kernelVolume)
	assert.Equal(t, &corev1.ImageVolumeSource{Reference: "kernel", PullPolicy: corev1.PullIfNotPresent}, kernelVolume.Image)
	assert.Equal(t, &corev1.VolumeMount{Name: "virtink-kernel", MountPath: "/mnt/virtink-kernel", ReadOnly: true}, findVolumeMount(vmContainer, "virtink-kernel"))

	imageVolume := findVolume(pod, "virtink-image-ubuntu")
	require.NotNil(t, imageVolume)
	assert.Equal(t, &corev1.ImageVolumeSource{Reference: "container-disk", PullPolicy: corev1.PullAlways}, imageVolume.Image)
	require.NotNil(t, findVolume(pod, "ubuntu"))
	assert.NotNil(t, findVolume(pod, "ubuntu").EmptyDir)

	// The backing file must be at the same path in the init and VM containers.
	imageVolumeMount := corev1.VolumeMount{Name: "virtink-image-ubuntu", MountPath: "/mnt/virtink-images/ubuntu", ReadOnly: true}
	assert.Equal(t, &imageVolumeMount, findVolumeMount(vmContainer, "virtink-image-ubuntu"))

	require.Len(t, pod.Spec.InitContainers, 1)
	initContainer := &pod.Spec.InitContainers[0]
	assert.Equal(t, "init-volume-ubuntu", initContainer.Name)
	assert.Equal(t, "prerunner", initContainer.Image)
	assert.Equal(t, []string{"virt-init-disk", "container-disk"}, initContainer.Command)
	assert.Equal(t, []string{"--image-dir", "/mnt/virtink-images/ubuntu", "--target", "/mnt/ubuntu/disk.qcow2"}, initContainer.Args)
	assert.Equal(t, &imageVolumeMount, findVolumeMount(initContainer, "virtink-image-ubuntu"))
	assert.Equal(t, "/mnt/ubuntu", findVolumeMount(initContainer, "ubuntu").MountPath)
}
