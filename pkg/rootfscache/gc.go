package rootfscache

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-logr/logr"
)

// GarbageCollector periodically removes the cache entries which are not used
// by any VM on the node and haven't been used for TTL.
//
// An entry is used by a VM if the VM's overlay in its Pod's emptyDir is backed
// by it, which is found by scanning the Pods' volumes on the node. The Pod's
// emptyDir is only removed by the kubelet once the Pod is gone.
type GarbageCollector struct {
	Cache    Cache
	PodsDir  string
	TTL      time.Duration
	Interval time.Duration
	Log      logr.Logger
}

// Start runs the garbage collector until ctx is done. It implements
// controller-runtime's manager.Runnable.
func (gc *GarbageCollector) Start(ctx context.Context) error {
	if gc.TTL <= 0 || gc.Interval <= 0 {
		return fmt.Errorf("TTL and interval of rootfs cache GC must be positive")
	}

	ticker := time.NewTicker(gc.Interval)
	defer ticker.Stop()
	for {
		removed, err := gc.Collect()
		if err != nil {
			gc.Log.Error(err, "failed to collect rootfs cache")
		}
		for _, key := range removed {
			gc.Log.Info("removed unused rootfs cache entry", "key", key)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Collect removes unused entries and leftovers of failed builds, and returns
// the keys of the removed entries.
func (gc *GarbageCollector) Collect() ([]string, error) {
	entries, err := os.ReadDir(gc.Cache.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	if err := gc.collectTmp(); err != nil {
		return nil, fmt.Errorf("remove leftover builds: %s", err)
	}

	// Entries in use are found before locking them, which doesn't race with
	// Cache.Prepare: it updates last-used and creates the overlay while holding
	// the entry's lock, so an entry used after this is not old enough to be
	// removed.
	inUse, err := gc.keysInUse()
	if err != nil {
		return nil, fmt.Errorf("find rootfs cache entries in use: %s", err)
	}

	var removed []string
	for _, entry := range entries {
		key := entry.Name()
		if !entry.IsDir() || key == locksDirName || key == tmpDirName || inUse[key] {
			continue
		}
		ok, err := gc.removeIfUnused(key)
		if err != nil {
			return removed, fmt.Errorf("remove rootfs cache entry %s: %s", key, err)
		}
		if ok {
			removed = append(removed, key)
		}
	}
	return removed, nil
}

func (gc *GarbageCollector) removeIfUnused(key string) (bool, error) {
	unlock, ok, err := gc.Cache.lock(key, true)
	if err != nil || !ok {
		return false, err
	}
	defer unlock()

	entryDir := gc.Cache.entryDir(key)
	fi, err := os.Stat(filepath.Join(entryDir, lastUsedFileName))
	if err != nil {
		if !os.IsNotExist(err) {
			return false, err
		}
		if fi, err = os.Stat(entryDir); err != nil {
			return false, err
		}
	}
	if time.Since(fi.ModTime()) < gc.TTL {
		return false, nil
	}
	return true, os.RemoveAll(entryDir)
}

// collectTmp removes the entries left in the temporary directory by builds
// which didn't finish, i.e. whose entries are not locked by a build.
func (gc *GarbageCollector) collectTmp() error {
	entries, err := os.ReadDir(filepath.Join(gc.Cache.Dir, tmpDirName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		i := strings.LastIndex(entry.Name(), "-")
		if i <= 0 {
			continue
		}
		unlock, ok, err := gc.Cache.lock(entry.Name()[:i], true)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		err = os.RemoveAll(filepath.Join(gc.Cache.Dir, tmpDirName, entry.Name()))
		unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

// keysInUse returns the keys of the entries backing the overlays of VMs on
// the node.
func (gc *GarbageCollector) keysInUse() (map[string]bool, error) {
	overlays, err := filepath.Glob(filepath.Join(gc.PodsDir, "*", "volumes", "kubernetes.io~empty-dir", "*", "rootfs.qcow2"))
	if err != nil {
		return nil, err
	}

	inUse := map[string]bool{}
	for _, overlay := range overlays {
		backingFile, err := ReadBackingFile(overlay)
		if err != nil {
			if os.IsNotExist(err) {
				// The Pod is being removed.
				continue
			}
			return nil, err
		}
		relPath, err := filepath.Rel(gc.Cache.Dir, backingFile)
		if err != nil || relPath == "." || strings.HasPrefix(relPath, "..") {
			continue
		}
		inUse[strings.SplitN(relPath, string(filepath.Separator), 2)[0]] = true
	}
	return inUse, nil
}

// ReadBackingFile returns the backing file name in the header of the qcow2
// image at path, or "" if it has none.
func ReadBackingFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// See https://gitlab.com/qemu-project/qemu/-/blob/master/docs/interop/qcow2.txt
	var header struct {
		Magic             uint32
		Version           uint32
		BackingFileOffset uint64
		BackingFileSize   uint32
	}
	if err := binary.Read(f, binary.BigEndian, &header); err != nil {
		return "", fmt.Errorf("read qcow2 header of %s: %s", path, err)
	}
	if header.Magic != 0x514649fb {
		return "", fmt.Errorf("%s is not a qcow2 image", path)
	}
	if header.BackingFileOffset == 0 {
		return "", nil
	}
	if header.BackingFileSize > 1023 {
		return "", fmt.Errorf("invalid backing file name size %d in %s", header.BackingFileSize, path)
	}

	name := make([]byte, header.BackingFileSize)
	if _, err := f.ReadAt(name, int64(header.BackingFileOffset)); err != nil {
		return "", fmt.Errorf("read backing file name of %s: %s", path, err)
	}
	return string(name), nil
}
