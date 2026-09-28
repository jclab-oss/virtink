// virt-init-disk runs in the init containers of a VM Pod to prepare the disks
// of volumes backed by image volumes.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

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
		target := fs.String("target", "", "Path of the qcow2 disk to create")
		fs.Parse(os.Args[2:])
		if *imageDir == "" || *image == "" || *size <= 0 || *target == "" {
			fs.Usage()
			os.Exit(2)
		}

		if *initPath != "" {
			if err := rootfscache.CheckInit(*imageDir, *initPath); err != nil {
				log.Fatalf("Image %s is not bootable: %s", *image, err)
			}
		}

		key, err := imageRootfsKey(*image, *imageDir, *size)
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

// imageRootfsKey returns the rootfs cache key of the image, preferring the
// image's digest over hashing its filesystem tree.
func imageRootfsKey(image string, imageDir string, size int64) (string, error) {
	if digest := rootfscache.DigestFromReference(image); digest != "" {
		return rootfscache.DigestKey(digest, size)
	}
	return rootfscache.TreeKey(imageDir, size)
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s container-disk|image-rootfs [flags]\n", os.Args[0])
	os.Exit(2)
}
