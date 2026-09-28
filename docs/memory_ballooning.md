# Memory Ballooning

Memory ballooning allows VMs to overcommit node memory. A VM is guaranteed a minimum amount of memory and can grow up to a maximum. Memory freed inside the guest is returned to the node automatically, and when the node runs short of memory, Virtink reclaims memory from VMs down to their minimum.

## Enabling Memory Ballooning

Memory ballooning is enabled by setting `spec.instance.memory.minSize`:

```yaml
apiVersion: virt.virtink.smartx.com/v1alpha1
kind: VirtualMachine
spec:
  instance:
    memory:
      minSize: 1Gi
      maxSize: 4Gi
```

- `minSize` is the amount of memory the guest keeps even when the node is under memory pressure.
- `maxSize` is the amount of memory the guest sees. It defaults to `size`, and `size` defaults to `maxSize`, so either one can be used. If both are set, they must be equal.

Without `minSize`, `maxSize` is only an alias of `size`, and the VM behaves as before.

`minSize` and `maxSize` may not be updated once the VM is created.

## How It Works

### Scheduling

Unless set explicitly, the VM Pod requests `minSize` plus a fixed overhead of 256Mi, and is limited to `maxSize` plus the same overhead. The scheduler only reserves `minSize` on the node, which is what allows memory to be overcommitted. A memory limit, if set explicitly, must not be less than `maxSize` plus the overhead, otherwise the VM would be OOM killed while growing.

### Returning Freed Memory

The VM is booted with a [virtio-balloon](https://github.com/cloud-hypervisor/cloud-hypervisor/blob/main/docs/balloon.md) device with free page reporting enabled. The guest kernel reports memory it no longer uses, and Cloud Hypervisor returns it to the node. The guest takes memory back from the node simply by using it, up to `maxSize`.

Linux only reports free contiguous blocks of 4MiB (order 10 with 4KiB pages), and the page cache keeps memory in use. The memory returned depends on the guest workload. See [Guest Tuning](#guest-tuning).

### Reclaiming Memory under Node Pressure

`virt-daemon` checks the node memory every 10 seconds. The node is considered under pressure when `MemAvailable / MemTotal` in `/proc/meminfo` drops below 20%, or the memory PSI `some avg10` in `/proc/pressure/memory` exceeds 10%.

Under pressure, `virt-daemon` inflates the balloon of every ballooning VM on the node:

1. The guest memory is first capped to the resident memory of the Cloud Hypervisor process. Since freed memory has already been returned, this approximates the memory the guest actually holds.
2. It is then reduced by 5% of `maxSize - minSize` (at least 64Mi) per interval, forcing the guest to give memory back, until the pressure is gone or `minSize` is reached.

Once available memory rises above 30%, balloons are deflated by the same step per interval until the guest has `maxSize` again. In between 20% and 30%, balloons are left untouched to avoid oscillation.

The balloon is created with `deflate_on_oom`, so a guest running out of memory can deflate the balloon by itself.

The memory currently available to the guest is reported in `status.memory.currentSize`:

```bash
kubectl get vm ubuntu-memory-ballooning -o jsonpath='{.status.memory.currentSize}'
```

Balloon resizes are logged by `virt-daemon` with the message `resized balloon`.

### Tuning

The thresholds can be changed with `virt-daemon` flags:

| Flag | Default | Description |
| --- | --- | --- |
| `--balloon-interval` | `10s` | The interval to adjust balloons. `0` disables automatic ballooning. |
| `--balloon-low-watermark` | `0.2` | Inflate balloons when the ratio of available node memory drops below this value. |
| `--balloon-high-watermark` | `0.3` | Deflate balloons when the ratio of available node memory rises above this value. |
| `--balloon-psi-threshold` | `10` | Inflate balloons when the node memory PSI `some avg10` (%) exceeds this value. |
| `--balloon-step-ratio` | `0.05` | The ratio of `maxSize - minSize` to resize balloons by per interval. |

Keep the low watermark well above the kubelet eviction threshold (`memory.available<100Mi` by default), so that balloons are inflated before the kubelet starts evicting Pods.

## Guest Requirements

- The guest must load the `virtio_balloon` driver. Without it, the balloon has no effect, and `status.memory.currentSize` stays at `maxSize`.
- Returning freed memory requires free page reporting, available in Linux 5.8 and later with `CONFIG_PAGE_REPORTING`. Most distribution kernels enable it.
- Windows guests with the virtio-win balloon driver do not support free page reporting. Their balloon is still inflated under node pressure, but freed memory is not returned automatically.

## Guest Tuning

The page cache keeps memory in use, so less memory is returned to the node. The following guest settings help:

- Enable MGLRU (`echo y > /sys/kernel/mm/lru_gen/enabled`, Linux 6.1 and later), and proactively reclaim cold memory with [DAMON_RECLAIM](https://docs.kernel.org/admin-guide/mm/damon/reclaim.html).
- Enable compaction proactiveness (`vm.compaction_proactiveness`), so that free memory forms blocks large enough to be reported.

## Limitations

The following can not be used with memory ballooning, and are rejected by the webhook:

- Hugepages, since hugepage memory can not be returned by the balloon.
- Dedicated CPU placement, since it requires the `Guaranteed` QoS class, which requires the memory request to equal the limit.
- SR-IOV and vDPA interfaces, since VFIO pins all guest memory.

Memory beyond `maxSize` can not be hot-plugged.
