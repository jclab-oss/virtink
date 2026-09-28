// Package qemuimg wraps the qemu-img commands used to prepare VM disks.
package qemuimg

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type ImageInfo struct {
	Format          string          `json:"format"`
	VirtualSize     int64           `json:"virtual-size"`
	BackingFilename string          `json:"backing-filename,omitempty"`
	FormatSpecific  *FormatSpecific `json:"format-specific,omitempty"`
}

type FormatSpecific struct {
	Type string `json:"type"`
	Data struct {
		DataFile string `json:"data-file,omitempty"`
	} `json:"data"`
}

func Info(path string) (*ImageInfo, error) {
	output, err := run("qemu-img", "info", "--output=json", path)
	if err != nil {
		return nil, err
	}
	var info ImageInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return nil, fmt.Errorf("parse qemu-img info output: %s", err)
	}
	return &info, nil
}

// ExternalFile returns the first file other than itself that the image
// references, or "" if the image is self-contained.
func (info *ImageInfo) ExternalFile() string {
	if info.BackingFilename != "" {
		return info.BackingFilename
	}
	if info.FormatSpecific != nil && info.FormatSpecific.Data.DataFile != "" {
		return info.FormatSpecific.Data.DataFile
	}
	return ""
}

// CreateOverlay creates a qcow2 image at path which is backed by backingFile.
// backingFile must be an absolute path, since it is resolved by Cloud
// Hypervisor in a different container.
func CreateOverlay(path string, backingFile string, backingFormat string) error {
	if !filepath.IsAbs(backingFile) {
		return fmt.Errorf("backing file %q is not an absolute path", backingFile)
	}
	return atomicCreate(path, func(tmpPath string) error {
		_, err := run("qemu-img", "create", "-q", "-f", "qcow2", "-F", backingFormat, "-b", backingFile, tmpPath)
		return err
	})
}

// Convert converts the image at src, whose format is srcFormat, to a
// standalone qcow2 image at path.
func Convert(path string, src string, srcFormat string) error {
	return atomicCreate(path, func(tmpPath string) error {
		_, err := run("qemu-img", "convert", "-f", srcFormat, "-O", "qcow2", src, tmpPath)
		return err
	})
}

func atomicCreate(path string, create func(tmpPath string) error) error {
	tmpPath := path + ".tmp"
	if err := os.RemoveAll(tmpPath); err != nil {
		return err
	}
	if err := create(tmpPath); err != nil {
		os.RemoveAll(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

func run(name string, arg ...string) ([]byte, error) {
	cmd := exec.Command(name, arg...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%q: %s: %s", cmd.String(), err, exitErr.Stderr)
		}
		return nil, fmt.Errorf("%q: %s", cmd.String(), err)
	}
	return output, nil
}
