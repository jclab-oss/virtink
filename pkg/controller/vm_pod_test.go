package controller

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	virtv1alpha1 "github.com/smartxworks/virtink/pkg/apis/virt/v1alpha1"
)

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

	kernelVolume := findPodVolume(pod, "virtink-kernel")
	require.NotNil(t, kernelVolume)
	assert.Equal(t, &corev1.ImageVolumeSource{Reference: "kernel", PullPolicy: corev1.PullIfNotPresent}, kernelVolume.Image)
	assert.Equal(t, &corev1.VolumeMount{Name: "virtink-kernel", MountPath: "/mnt/virtink-kernel", ReadOnly: true}, findVolumeMount(vmContainer, "virtink-kernel"))

	imageVolume := findPodVolume(pod, "virtink-image-ubuntu")
	require.NotNil(t, imageVolume)
	assert.Equal(t, &corev1.ImageVolumeSource{Reference: "container-disk", PullPolicy: corev1.PullAlways}, imageVolume.Image)
	require.NotNil(t, findPodVolume(pod, "ubuntu"))
	assert.NotNil(t, findPodVolume(pod, "ubuntu").EmptyDir)

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

func TestBuildVMPodWithImageRootfs(t *testing.T) {
	vm := &virtv1alpha1.VirtualMachine{
		Spec: virtv1alpha1.VirtualMachineSpec{
			Instance: virtv1alpha1.Instance{
				Kernel: &virtv1alpha1.Kernel{
					Image:   "kernel",
					Cmdline: "console=ttyS0 root=/dev/vda rw init=/lib/systemd/systemd",
				},
				Disks: []virtv1alpha1.Disk{{Name: "root"}, {Name: "data"}},
			},
			Volumes: []virtv1alpha1.Volume{{
				Name: "root",
				VolumeSource: virtv1alpha1.VolumeSource{
					ImageRootfs: &virtv1alpha1.ImageRootfsVolumeSource{
						Image: "ubuntu",
						Size:  resource.MustParse("4Gi"),
					},
				},
			}, {
				Name: "data",
				VolumeSource: virtv1alpha1.VolumeSource{
					ImageRootfs: &virtv1alpha1.ImageRootfsVolumeSource{
						Image: "data",
						Size:  resource.MustParse("1Gi"),
					},
				},
			}},
		},
	}

	r := &VMReconciler{PrerunnerImageName: "prerunner"}
	pod, err := r.buildVMPod(context.Background(), vm)
	require.NoError(t, err)
	vmContainer := &pod.Spec.Containers[0]

	cacheVolumes := 0
	for _, volume := range pod.Spec.Volumes {
		if volume.Name == "virtink-rootfs-cache" {
			cacheVolumes++
			assert.Equal(t, "/var/lib/virtink/rootfs-cache", volume.HostPath.Path)
		}
	}
	assert.Equal(t, 1, cacheVolumes)
	assert.Equal(t, &corev1.VolumeMount{Name: "virtink-rootfs-cache", MountPath: "/var/lib/virtink/rootfs-cache", ReadOnly: true}, findVolumeMount(vmContainer, "virtink-rootfs-cache"))
	// The VM only needs the rootfs in the cache, not the image.
	assert.Nil(t, findVolumeMount(vmContainer, "virtink-image-root"))
	assert.Equal(t, "/mnt/root", findVolumeMount(vmContainer, "root").MountPath)

	require.Len(t, pod.Spec.InitContainers, 2)
	initContainer := &pod.Spec.InitContainers[0]
	assert.Equal(t, "init-volume-root", initContainer.Name)
	assert.Equal(t, []string{"virt-init-disk", "image-rootfs"}, initContainer.Command)
	assert.Equal(t, []string{
		"--image-dir", "/mnt/virtink-images/root",
		"--image", "ubuntu",
		"--size", "4294967296",
		"--init", "/lib/systemd/systemd",
		"--cache-dir", "/var/lib/virtink/rootfs-cache",
		"--target", "/mnt/root/rootfs.qcow2",
	}, initContainer.Args)
	assert.Equal(t, &corev1.VolumeMount{Name: "virtink-image-root", MountPath: "/mnt/virtink-images/root", ReadOnly: true}, findVolumeMount(initContainer, "virtink-image-root"))
	assert.Equal(t, &corev1.VolumeMount{Name: "virtink-rootfs-cache", MountPath: "/var/lib/virtink/rootfs-cache"}, findVolumeMount(initContainer, "virtink-rootfs-cache"))

	// A disk that isn't booted from doesn't need an init.
	assert.Contains(t, strings.Join(pod.Spec.InitContainers[1].Args, " "), "--init  ")
}
