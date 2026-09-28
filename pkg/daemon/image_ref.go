package daemon

import (
	"fmt"
	"os"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"

	virtv1alpha1 "github.com/smartxworks/virtink/pkg/apis/virt/v1alpha1"
	"github.com/smartxworks/virtink/pkg/rootfscache"
)

// kubeletPodsDir is a variable for tests.
var kubeletPodsDir = "/var/lib/kubelet/pods"

// reconcileImageRefs passes the digests of the images of imageRootfs volumes
// to their init containers, which use them as rootfs cache keys.
//
// The digest of an image volume is only known once it's mounted, and is only
// reported in the Pod status (with the ImageVolumeWithDigest feature gate),
// which the init container can't read. So it's written to a file in the
// volume's emptyDir once the init container is running: the reported image
// ref, or an empty file if the kubelet doesn't report it.
func (r *VMReconciler) reconcileImageRefs(vm *virtv1alpha1.VirtualMachine, vmPod *corev1.Pod) error {
	for _, volume := range vm.Spec.Volumes {
		if volume.ImageRootfs == nil || rootfscache.DigestFromReference(volume.ImageRootfs.Image) != "" {
			continue
		}

		var containerStatus *corev1.ContainerStatus
		for i := range vmPod.Status.InitContainerStatuses {
			if vmPod.Status.InitContainerStatuses[i].Name == "init-volume-"+volume.Name {
				containerStatus = &vmPod.Status.InitContainerStatuses[i]
			}
		}
		// The mounts of a container are reported once it's running.
		if containerStatus == nil || containerStatus.State.Running == nil || len(containerStatus.VolumeMounts) == 0 {
			continue
		}

		path := filepath.Join(kubeletPodsDir, string(vmPod.UID), "volumes/kubernetes.io~empty-dir", volume.Name, rootfscache.ImageRefFileName)
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return err
		}

		var imageRef string
		for _, mountStatus := range containerStatus.VolumeMounts {
			if mountStatus.Name == "virtink-image-"+volume.Name && mountStatus.VolumeStatus != nil && mountStatus.VolumeStatus.Image != nil {
				imageRef = mountStatus.VolumeStatus.Image.ImageRef
			}
		}
		if err := writeFileAtomically(path, []byte(imageRef)); err != nil {
			return fmt.Errorf("write image ref of volume %q: %s", volume.Name, err)
		}
	}
	return nil
}

func writeFileAtomically(path string, data []byte) error {
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
