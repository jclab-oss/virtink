# Disks and Volumes

Making persistent storage in the cluster (volumes) accessible to VMs consists of two parts. First, volumes are specified in `spec.volumes`. Then, disks stored by the volumes are added to the VM by specifying them in `spec.instance.disks`. Each disk must have a corresponding volume with the same name.

## Disks

VM disks are configured in `spec.instance.disks`. At this time, a disk only has two properties: a required and unique `name` that matches a volume name in `spec.volumes`, and an optional `readonly` field to specify whether this disk should be readonly to the VM.

CD-ROMs or floppy disks are not supported by Virtink.

## Volumes

Volumes are configured in `spec.volumes`. Each volume should has a unique name and a valid volume source. Supported volume sources are:

- [`containerDisk`](#containerdisk-volume)
- [`cloudInit`](#cloudinit-volume)
- [`imageRootfs`](#imagerootfs-volume)
- [`containerRootfs`](#containerrootfs-volume) (deprecated)
- [`persistentVolumeClaim`](#persistentvolumeclaim-volume)
- [`dataVolume`](#datavolume-volume)

### `containerDisk` Volume

The `containerDisk` feature provides the ability to store and distribute VM disks in the container image registry. No network shared storage devices are utilized by `containerDisk`s. The disks are pulled from the container registry and reside on the local node hosting the VMs that consume the disks.

#### When to Use a `containerDisk`

`containerDisk`s are ephemeral storage devices that can be assigned to any number of active VMs. This makes them an ideal tool for users who want to replicate a large number of VM workloads that do not require persistent data.

#### When to Not Use a `containerDisk`

`containerDisk`s are not a good solution for any workload that requires persistent root disks across VM restarts.

#### How a `containerDisk` Is Used

The image is mounted into the VM Pod as a read-only [image volume](https://kubernetes.io/docs/concepts/storage/volumes/#image), so it's pulled and stored only once per node. Raw and QCOW2 disks are not copied: each VM gets a QCOW2 overlay in its Pod's `emptyDir` with the disk in the image as its backing file, and only the blocks written by the VM are stored in the overlay. Disks in other formats supported by `qemu-img` are converted into the VM's Pod when the VM starts.

A disk referencing other files, such as a QCOW2 image with a backing file or an external data file, is rejected.

#### `containerDisk` Workflow Example

Disks must be placed at exactly the `/disk` path. Alternatively, `/disk` can be a directory containing exactly one disk file, which is the layout of KubeVirt's container disks. Raw and QCOW2 formats are recommended so that disks are shared rather than copied, and QCOW2 (optionally compressed) reduces the container image's size. The image needs nothing but the disk, and its entrypoint is never run.

Below is an example of injecting a remote VM disk image into a container image:

```dockerfile
FROM scratch
ADD https://cloud-images.ubuntu.com/jammy/current/jammy-server-cloudimg-amd64.img /disk
```

### `cloudInit` Volume

A `cloudInit` volume allows attaching cloud-init data-sources to the VM. If the VM contains a proper cloud-init setup, it will pick up the disk as a user-data source.

Below is an example of embedding cloud-init user-data directly in the VM spec:

```yaml
apiVersion: virt.virtink.smartx.com/v1alpha1
kind: VirtualMachine
spec:
  instance:
    disks:
      - name: ubuntu
      - name: cloud-init
  volumes:
    - name: ubuntu
      containerDisk:
        image: smartxworks/virtink-container-disk-ubuntu
    - name: cloud-init
      cloudInit:
        userData: |-
          #cloud-config
          password: password
          chpasswd: { expire: False }
          ssh_pwauth: True
```

You can also use `userDataBase64` if you prefer to use the Base64 encoded version, or use `userDataSecretName` to move cloud-init data outside the VM spec and wrap them in a Secret.

### `imageRootfs` Volume

The `imageRootfs` feature boots a VM from the filesystem of an ordinary container image, such as an image built `FROM ubuntu:jammy`. The image's whole filesystem becomes the VM's rootfs, with no further requirements of disk partitions or file system formatting. Since the image is used as-is, it must be bootable: it must contain an init (by default `/sbin/init`, or the one given with `init=` in the kernel `cmdline`), such as systemd. The image's entrypoint and other configuration are not used.

An image rootfs is not bootable by a firmware, so it's used with Virtink's [direct kernel boot](direct_kernel_boot.md) feature, which is required. Below is an example that directly boots a VM with a given kernel and an `imageRootfs` disk:

```yaml
apiVersion: virt.virtink.smartx.com/v1alpha1
kind: VirtualMachine
spec:
  instance:
    kernel:
      image: smartxworks/virtink-kernel-5.15.12
      cmdline: "console=ttyS0 root=/dev/vda rw"
    disks:
      - name: ubuntu
  volumes:
    - name: ubuntu
      imageRootfs:
        image: smartxworks/virtink-image-rootfs-ubuntu
        size: 4Gi
```

#### How an `imageRootfs` Is Used

The image is mounted into the VM Pod as a read-only [image volume](https://kubernetes.io/docs/concepts/storage/volumes/#image). When a VM starts, an ext4 disk of the given `size` is built from the image into a cache on the node at `/var/lib/virtink/rootfs-cache`, unless the cache already has one. Each VM then gets a QCOW2 overlay in its Pod's `emptyDir` with the cached disk as its backing file, so only the blocks written by the VM are stored per VM.

Cached disks are identified by, in order of preference:

1. The digest in the image reference, e.g. `smartxworks/virtink-image-rootfs-ubuntu@sha256:...`. Pin images by digest to skip reading the image when the disk is cached.
2. The digest of the mounted image reported in the Pod status. This requires the `ImageVolumeWithDigest` feature gate (alpha since Kubernetes v1.35) to be enabled, and the container runtime to report the digests of image volumes, which neither containerd (as of v2.3) nor CRI-O supports yet.
3. A hash of the image's filesystem, which is computed by reading the whole image each time a VM starts.

Cached disks that are not used by any VM on the node are removed after 24 hours by `virt-daemon`, which can be changed with its `--rootfs-cache-ttl` flag. Note that the cache is not accounted for in the ephemeral storage of Pods.

#### When to Use an `imageRootfs`

`imageRootfs`s are ephemeral storage devices that can be assigned to any number of active VMs. This makes them an ideal tool for users who want to replicate a large number of VM workloads that do not require persistent data.

#### When to Not Use an `imageRootfs`

`imageRootfs`s are not a good solution for any workload that requires persistent root disks across VM restarts. VMs with `imageRootfs` volumes can't be migrated.

#### `imageRootfs` Workflow Example

Packages like systemd, cloud-init and openssh-server should be installed to make the image a valid and useful VM rootfs. Below is an example of building a Ubuntu VM rootfs image based on the Ubuntu container image. Since `/etc/resolv.conf` is managed by the image builder, it's replaced in a separate stage:

```dockerfile
FROM ubuntu:jammy AS rootfs
RUN apt-get update -y && \
    apt-get install -y --no-install-recommends systemd-sysv udev lsb-release cloud-init sudo openssh-server && \
    rm -rf /var/lib/apt/lists/*

FROM alpine AS fixup
COPY --from=rootfs / /rootfs
RUN ln -sf ../run/systemd/resolve/stub-resolv.conf /rootfs/etc/resolv.conf

FROM scratch
COPY --from=fixup /rootfs /
```

### `containerRootfs` Volume

**Deprecated**: Use [`imageRootfs`](#imagerootfs-volume) instead, which uses the image itself as the rootfs and shares it between VMs. A `containerRootfs` image can be converted by making `/rootfs` the root of the image, for example by replacing `FROM smartxworks/virtink-container-rootfs-base` and `COPY --from=rootfs / /rootfs` with `FROM scratch` and `COPY --from=rootfs / /`.

The `containerRootfs` feature provides the ability to store and distribute VM rootfs in the container image registry. No network shared storage devices are utilized by `containerRootfs`s. The disks are pulled from the container registry and reside on the local node hosting the VMs that consume the disks.

Unlike `containerDisk`s, which require using of a raw or QCOW2 image. A `containerRootfs` volume can be built solely with Docker or other container image building tools. The rootfs on the container image will be used directly as the VM's rootfs, with no further requirements of disk partitions or file system formatting.

However, since normally a `containerRootfs` is not bootable itself, it's mostly used with Virtink's [direct kernel boot](direct_kernel_boot.md) feature. Below is an example that directly boots a VM with a given kernel and a `containerRootfs` disk:

```yaml
apiVersion: virt.virtink.smartx.com/v1alpha1
kind: VirtualMachine
spec:
  instance:
    kernel:
      image: smartxworks/virtink-kernel-5.15.12
      cmdline: "console=ttyS0 root=/dev/vda rw"
    disks:
      - name: ubuntu
  volumes:
    - name: ubuntu
      containerRootfs:
        image: smartxworks/virtink-container-rootfs-ubuntu
        size: 4Gi
```

#### When to Use a `containerRootfs`

`containerRootfs`s are ephemeral storage devices that can be assigned to any number of active VMs. This makes them an ideal tool for users who want to replicate a large number of VM workloads that do not require persistent data.

#### When to Not Use a `containerRootfs`

`containerRootfs`s are not a good solution for any workload that requires persistent root disks across VM restarts.

#### `containerRootfs` Workflow Example

The root of the rootfs must be placed at exactly the `/rootfs` path. `containerRootfs`s must be based on `smartxworks/virtink-container-rootfs-base`. Packages like systemd, cloud-init and openssh-server should be installed to make it a valid and useful VM rootfs.

Below is an example of building a Ubuntu VM rootfs based on the Ubuntu container image and injecting it to into a container image:

```dockerfile
FROM ubuntu:jammy AS rootfs
RUN apt-get update -y && \
    apt-get install -y --no-install-recommends systemd-sysv udev lsb-release cloud-init sudo openssh-server && \
    rm -rf /var/lib/apt/lists/*

FROM smartxworks/virtink-container-rootfs-base
COPY --from=rootfs / /rootfs
RUN ln -sf ../run/systemd/resolve/stub-resolv.conf /rootfs/etc/resolv.conf
```

### `persistentVolumeClaim` Volume

A `persistentVolumeClaim` volume allows connecting a PVC to a VM disk. Use a `persistentVolumeClaim` volume when the VM's disk needs to persist after the VM terminates. This allows for the VM's data to remain persistent between restarts.

A PV can be in `Filesystem` or `Block` mode:

- `Filesystem` mode: For Virtink to be able to consume the disk present on a PV's filesystem, the disk must be named `disk.img` and be placed in the root path of the filesystem. Currently the disk is also required to be in raw format.
- `Block` mode: Use a block volume for consuming raw block devices.

A simple example which attaches a PVC as a disk may look like this:

```yaml
apiVersion: virt.virtink.smartx.com/v1alpha1
kind: VirtualMachine
spec:
  instance:
    disks:
      - name: ubuntu
  volumes:
    - name: ubuntu
      persistentVolumeClaim:
        claimName: ubuntu
```

### `dataVolume` Volume

A DataVolume is a custom resource provided by the [Containerized Data Importer (CDI) project](https://github.com/kubevirt/containerized-data-importer). Virtink integrates with CDI in order to provide users a workflow for dynamically creating PVCs and importing data into those PVCs. Without using a DataVolume, users have to prepare a PVC with a disk image before assigning it to a VM manifest. With a DataVolume, both the PVC creation and import is automated on behalf of the user.

### Enabling DataVolume Support

In order to take advantage of the `dataVolume` volume source on a VM, CDI must be installed. Please refer to [CDI project documentation](https://github.com/kubevirt/containerized-data-importer#deploy-it) for its installation and usage.

### Creating a VM with `dataVolume` Disk

Below is an example of a DataVolume being referenced by a VM. It is okay to post the VM manifest to the cluster before the DataVolume is created or while the DataVolume is still having data imported. Virtink knows not to start the VM until all referenced DataVolumes have finished their clone and import phases.

```yaml
apiVersion: virt.virtink.smartx.com/v1alpha1
kind: VirtualMachine
spec:
  instance:
    disks:
      - name: ubuntu
  volumes:
    - name: ubuntu
      dataVolume:
        volumeName: ubuntu
```
