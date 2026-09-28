// virt-init-disk runs in the init containers of a VM Pod to prepare the disks
// of volumes backed by image volumes.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/smartxworks/virtink/pkg/containerdisk"
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
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s container-disk [flags]\n", os.Args[0])
	os.Exit(2)
}
