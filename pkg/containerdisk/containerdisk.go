// Package containerdisk prepares the disk of a containerDisk volume from the
// container image mounted as an image volume.
package containerdisk

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/smartxworks/virtink/pkg/qemuimg"
)

// FindDiskFile returns the disk file in imageDir, which is the root of a
// mounted containerDisk image. The disk is either the file /disk (virtink
// layout) or the only file in the /disk directory (KubeVirt layout).
func FindDiskFile(imageDir string) (string, error) {
	diskPath := filepath.Join(imageDir, "disk")
	fi, err := os.Lstat(diskPath)
	if err != nil {
		return "", fmt.Errorf("find disk: %s", err)
	}
	if fi.Mode().IsRegular() {
		return diskPath, nil
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("%s is neither a regular file nor a directory", diskPath)
	}

	entries, err := os.ReadDir(diskPath)
	if err != nil {
		return "", fmt.Errorf("find disk: %s", err)
	}
	var files []string
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			files = append(files, filepath.Join(diskPath, entry.Name()))
		}
	}
	if len(files) != 1 {
		return "", fmt.Errorf("%s must contain exactly 1 regular file, found %d", diskPath, len(files))
	}
	return files[0], nil
}

// Prepare creates the VM's writable qcow2 disk at targetPath from the image
// mounted at imageDir.
//
// Raw and qcow2 disks are used in place as the backing file of targetPath, so
// that they are shared by every VM using the image on the node. Other formats
// are converted into targetPath.
func Prepare(imageDir string, targetPath string) error {
	diskPath, err := FindDiskFile(imageDir)
	if err != nil {
		return err
	}

	info, err := qemuimg.Info(diskPath)
	if err != nil {
		return err
	}
	// The image is not trusted. Cloud Hypervisor follows backing files once
	// they are enabled, so an image referencing another file could expose any
	// file readable by Cloud Hypervisor to the guest.
	if externalFile := info.ExternalFile(); externalFile != "" {
		return fmt.Errorf("disk %s must not reference external file %q", diskPath, externalFile)
	}

	switch info.Format {
	case "raw", "qcow2":
		return qemuimg.CreateOverlay(targetPath, diskPath, info.Format)
	default:
		return qemuimg.Convert(targetPath, diskPath, info.Format)
	}
}
