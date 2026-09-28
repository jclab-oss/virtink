package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	virtv1alpha1 "github.com/smartxworks/virtink/pkg/apis/virt/v1alpha1"
)

func TestReconcileImageRefs(t *testing.T) {
	kubeletPodsDir = t.TempDir()
	defer func() { kubeletPodsDir = "/var/lib/kubelet/pods" }()

	imageRootfs := func(image string) virtv1alpha1.VolumeSource {
		return virtv1alpha1.VolumeSource{ImageRootfs: &virtv1alpha1.ImageRootfsVolumeSource{Image: image, Size: resource.MustParse("4Gi")}}
	}
	vm := &virtv1alpha1.VirtualMachine{
		Spec: virtv1alpha1.VirtualMachineSpec{
			Volumes: []virtv1alpha1.Volume{
				{Name: "reported", VolumeSource: imageRootfs("ubuntu")},
				{Name: "unreported", VolumeSource: imageRootfs("debian")},
				{Name: "pinned", VolumeSource: imageRootfs("fedora@sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8")},
				{Name: "pending", VolumeSource: imageRootfs("alpine")},
			},
		},
	}
	running := corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	vmPod := &corev1.Pod{
		Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{{
				Name:  "init-volume-reported",
				State: running,
				VolumeMounts: []corev1.VolumeMountStatus{{Name: "reported"}, {
					Name: "virtink-image-reported",
					VolumeStatus: &corev1.VolumeStatus{
						Image: &corev1.ImageVolumeStatus{ImageRef: "docker.io/library/ubuntu@sha256:abc"},
					},
				}},
			}, {
				Name:         "init-volume-unreported",
				State:        running,
				VolumeMounts: []corev1.VolumeMountStatus{{Name: "unreported"}, {Name: "virtink-image-unreported"}},
			}, {
				Name:         "init-volume-pinned",
				State:        running,
				VolumeMounts: []corev1.VolumeMountStatus{{Name: "virtink-image-pinned"}},
			}, {
				Name:  "init-volume-pending",
				State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{}},
			}},
		},
	}
	vmPod.UID = "pod-uid"
	for _, volume := range vm.Spec.Volumes {
		require.NoError(t, os.MkdirAll(filepath.Join(kubeletPodsDir, "pod-uid/volumes/kubernetes.io~empty-dir", volume.Name), 0755))
	}

	r := &VMReconciler{}
	require.NoError(t, r.reconcileImageRefs(vm, vmPod))

	imageRefPath := func(volume string) string {
		return filepath.Join(kubeletPodsDir, "pod-uid/volumes/kubernetes.io~empty-dir", volume, ".virtink-image-ref")
	}
	data, err := os.ReadFile(imageRefPath("reported"))
	assert.NoError(t, err)
	assert.Equal(t, "docker.io/library/ubuntu@sha256:abc", string(data))
	data, err = os.ReadFile(imageRefPath("unreported"))
	assert.NoError(t, err)
	assert.Empty(t, data)
	assert.NoFileExists(t, imageRefPath("pinned"))
	assert.NoFileExists(t, imageRefPath("pending"))

	// The file isn't rewritten once it's written.
	vmPod.Status.InitContainerStatuses[0].VolumeMounts[1].VolumeStatus.Image.ImageRef = "changed"
	require.NoError(t, r.reconcileImageRefs(vm, vmPod))
	data, err = os.ReadFile(imageRefPath("reported"))
	assert.NoError(t, err)
	assert.Equal(t, "docker.io/library/ubuntu@sha256:abc", string(data))
}
