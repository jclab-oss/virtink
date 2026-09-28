// virt-init-disk runs in the init containers of a VM Pod to prepare the disks
// of volumes backed by image volumes.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/smartxworks/virtink/pkg/containerdisk"
	"github.com/smartxworks/virtink/pkg/rootfscache"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}

	switch os.Args[1] {
	case "container-disk":
		fs := flag.NewFlagSet("container-disk", flag.ExitOnError)
		imageDir := fs.String("image-dir", "", "Directory where the containerDisk image is mounted")
		target := fs.String("target", "", "Path of the qcow2 disk to create")
		fs.Parse(os.Args[2:])
		if *imageDir == "" || *target == "" {
			fs.Usage()
			os.Exit(2)
		}
		if err := checkImageMounted(*imageDir); err != nil {
			log.Fatal(err)
		}

		if err := containerdisk.Prepare(*imageDir, *target); err != nil {
			log.Fatalf("Failed to prepare container disk: %s", err)
		}
	case "image-rootfs":
		fs := flag.NewFlagSet("image-rootfs", flag.ExitOnError)
		imageDir := fs.String("image-dir", "", "Directory where the image is mounted")
		image := fs.String("image", "", "Reference of the image")
		size := fs.Int64("size", 0, "Size of the rootfs disk in bytes")
		initPath := fs.String("init", "", "Path of init in the image to check for, if the rootfs is booted from")
		cacheDir := fs.String("cache-dir", rootfscache.DefaultDir, "Directory of the rootfs cache")
		imageRefFile := fs.String("image-ref-file", "", "File to wait for the image ref of the mounted image in, if the image reference is not pinned by digest")
		imageRefTimeout := fs.Duration("image-ref-timeout", time.Minute, "How long to wait for the image ref file")
		target := fs.String("target", "", "Path of the qcow2 disk to create")
		fs.Parse(os.Args[2:])
		if *imageDir == "" || *image == "" || *size <= 0 || *target == "" {
			fs.Usage()
			os.Exit(2)
		}
		if err := checkImageMounted(*imageDir); err != nil {
			log.Fatal(err)
		}

		if *initPath != "" {
			if err := rootfscache.CheckInit(*imageDir, *initPath); err != nil {
				log.Fatalf("Image %s is not bootable: %s", *image, err)
			}
		}

		key, err := imageRootfsKey(*image, *imageRefFile, *imageRefTimeout, *imageDir, *size)
		if err != nil {
			log.Fatalf("Failed to get rootfs cache key: %s", err)
		}
		log.Printf("Using rootfs cache entry %s", key)

		cache := rootfscache.Cache{Dir: *cacheDir}
		if err := cache.Prepare(key, *imageDir, *size, *target); err != nil {
			log.Fatalf("Failed to prepare image rootfs: %s", err)
		}
	default:
		usage()
	}
}

// checkImageMounted returns an error if imageDir, where an image volume is
// mounted, is empty. A container runtime without image volume support mounts
// an empty directory instead of failing, which would otherwise surface as a
// confusing error about a file missing from the image.
func checkImageMounted(imageDir string) error {
	entries, err := os.ReadDir(imageDir)
	if err != nil {
		return fmt.Errorf("read image volume %s: %s", imageDir, err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("image volume %s is empty: the container runtime of the node may not support image volumes (containerd >= 2.1 is required)", imageDir)
	}
	return nil
}

// imageRootfsKey returns the rootfs cache key of the image. It uses, in order
// of preference, the digest the image reference is pinned to, the digest of
// the mounted image reported by the kubelet, and a hash of the filesystem.
func imageRootfsKey(image string, imageRefFile string, imageRefTimeout time.Duration, imageDir string, size int64) (string, error) {
	if digest := rootfscache.DigestFromReference(image); digest != "" {
		return rootfscache.DigestKey(digest, size)
	}
	if imageRefFile != "" {
		imageRef, err := waitForImageRef(imageRefFile, imageRefTimeout)
		if err != nil {
			return "", err
		}
		if digest := rootfscache.DigestFromImageRef(imageRef); digest != "" {
			return rootfscache.DigestKey(digest, size)
		}
		log.Printf("The digest of image %s is not reported by the kubelet, hashing its filesystem", image)
	}
	return rootfscache.TreeKey(imageDir, size)
}

// waitForImageRef waits for virt-daemon to write the image ref reported in the
// Pod status to path. It returns "" if the file doesn't appear in time.
func waitForImageRef(path string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			return strings.TrimSpace(string(data)), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		if time.Now().After(deadline) {
			log.Printf("Timed out waiting for %s", path)
			return "", nil
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s container-disk|image-rootfs [flags]\n", os.Args[0])
	os.Exit(2)
}
