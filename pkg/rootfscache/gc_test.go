package rootfscache

import (
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeOverlay writes the header of a qcow2 image backed by backingFile, as a
// VM's overlay in the emptyDir of a Pod.
func writeOverlay(t *testing.T, podsDir string, podUID string, backingFile string) {
	path := filepath.Join(podsDir, podUID, "volumes", "kubernetes.io~empty-dir", "root", "rootfs.qcow2")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))

	header := make([]byte, 512)
	binary.BigEndian.PutUint32(header[0:], 0x514649fb)
	binary.BigEndian.PutUint32(header[4:], 3)
	binary.BigEndian.PutUint64(header[8:], 104)
	binary.BigEndian.PutUint32(header[16:], uint32(len(backingFile)))
	copy(header[104:], backingFile)
	require.NoError(t, os.WriteFile(path, header, 0644))
}

func writeEntry(t *testing.T, cache Cache, key string, lastUsed time.Time) {
	entryDir := cache.entryDir(key)
	require.NoError(t, os.MkdirAll(entryDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(entryDir, baseFileName), nil, 0600))
	require.NoError(t, os.WriteFile(filepath.Join(entryDir, lastUsedFileName), nil, 0600))
	require.NoError(t, os.Chtimes(filepath.Join(entryDir, lastUsedFileName), lastUsed, lastUsed))
}

func TestReadBackingFile(t *testing.T) {
	podsDir := t.TempDir()
	writeOverlay(t, podsDir, "pod", "/var/lib/virtink/rootfs-cache/key/base.raw")
	backingFile, err := ReadBackingFile(filepath.Join(podsDir, "pod/volumes/kubernetes.io~empty-dir/root/rootfs.qcow2"))
	assert.NoError(t, err)
	assert.Equal(t, "/var/lib/virtink/rootfs-cache/key/base.raw", backingFile)

	notQcow2 := filepath.Join(t.TempDir(), "disk.raw")
	require.NoError(t, os.WriteFile(notQcow2, make([]byte, 512), 0644))
	_, err = ReadBackingFile(notQcow2)
	assert.Error(t, err)

	if _, err := exec.LookPath("qemu-img"); err == nil {
		dir := t.TempDir()
		require.NoError(t, exec.Command("qemu-img", "create", "-q", "-f", "raw", filepath.Join(dir, "base.raw"), "1M").Run())
		require.NoError(t, exec.Command("qemu-img", "create", "-q", "-f", "qcow2", "-F", "raw", "-b", filepath.Join(dir, "base.raw"), filepath.Join(dir, "overlay.qcow2")).Run())
		backingFile, err := ReadBackingFile(filepath.Join(dir, "overlay.qcow2"))
		assert.NoError(t, err)
		assert.Equal(t, filepath.Join(dir, "base.raw"), backingFile)

		require.NoError(t, exec.Command("qemu-img", "create", "-q", "-f", "qcow2", filepath.Join(dir, "standalone.qcow2"), "1M").Run())
		backingFile, err = ReadBackingFile(filepath.Join(dir, "standalone.qcow2"))
		assert.NoError(t, err)
		assert.Equal(t, "", backingFile)
	}
}

func TestCollect(t *testing.T) {
	cache := Cache{Dir: t.TempDir()}
	podsDir := t.TempDir()
	gc := GarbageCollector{Cache: cache, PodsDir: podsDir, TTL: time.Hour}
	old := time.Now().Add(-2 * time.Hour)

	writeEntry(t, cache, "in-use", old)
	writeOverlay(t, podsDir, "pod-1", filepath.Join(cache.Dir, "in-use", baseFileName))
	writeEntry(t, cache, "unused-old", old)
	writeEntry(t, cache, "unused-recent", time.Now())
	writeEntry(t, cache, "locked-old", old)
	unlock, ok, err := cache.lock("locked-old", false)
	require.NoError(t, err)
	require.True(t, ok)
	// Overlays backed by other files are ignored.
	writeOverlay(t, podsDir, "pod-2", "/mnt/virtink-images/disk/disk")

	require.NoError(t, os.MkdirAll(filepath.Join(cache.Dir, tmpDirName, "failed-build-123"), 0700))
	require.NoError(t, os.MkdirAll(filepath.Join(cache.Dir, tmpDirName, "locked-old-456"), 0700))

	removed, err := gc.Collect()
	require.NoError(t, err)
	assert.Equal(t, []string{"unused-old"}, removed)
	assert.DirExists(t, cache.entryDir("in-use"))
	assert.NoDirExists(t, cache.entryDir("unused-old"))
	assert.DirExists(t, cache.entryDir("unused-recent"))
	assert.DirExists(t, cache.entryDir("locked-old"))
	assert.NoDirExists(t, filepath.Join(cache.Dir, tmpDirName, "failed-build-123"))
	assert.DirExists(t, filepath.Join(cache.Dir, tmpDirName, "locked-old-456"))

	unlock()
	require.NoError(t, os.RemoveAll(filepath.Join(podsDir, "pod-1")))
	removed, err = gc.Collect()
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"in-use", "locked-old"}, removed)
	assert.NoDirExists(t, filepath.Join(cache.Dir, tmpDirName, "locked-old-456"))
}

func TestCollectPreparedEntry(t *testing.T) {
	requireTools(t)

	root := writeTree(t)
	cache := Cache{Dir: t.TempDir()}
	podsDir := t.TempDir()
	gc := GarbageCollector{Cache: cache, PodsDir: podsDir, TTL: time.Nanosecond}

	key, err := TreeKey(root, 64<<20)
	require.NoError(t, err)
	target := filepath.Join(podsDir, "pod", "volumes", "kubernetes.io~empty-dir", "root", "rootfs.qcow2")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0755))
	require.NoError(t, cache.Prepare(key, root, 64<<20, target))

	removed, err := gc.Collect()
	require.NoError(t, err)
	assert.Empty(t, removed)

	require.NoError(t, os.Remove(target))
	removed, err = gc.Collect()
	require.NoError(t, err)
	assert.Equal(t, []string{key}, removed)
}
