package rootfscache

import (
	"crypto/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/smartxworks/virtink/pkg/qemuimg"
)

const testDigest = "sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8"

func TestDigestFromReference(t *testing.T) {
	assert.Equal(t, testDigest, DigestFromReference("ubuntu@"+testDigest))
	assert.Equal(t, testDigest, DigestFromReference("registry:5000/os/ubuntu:jammy@"+testDigest))
	assert.Equal(t, "", DigestFromReference("ubuntu"))
	assert.Equal(t, "", DigestFromReference("registry:5000/os/ubuntu:jammy"))
	assert.Equal(t, "", DigestFromReference("ubuntu@sha256:invalid"))
}

func TestDigestFromImageRef(t *testing.T) {
	assert.Equal(t, testDigest, DigestFromImageRef(testDigest))
	assert.Equal(t, testDigest, DigestFromImageRef("docker.io/library/ubuntu@"+testDigest))
	assert.Equal(t, "", DigestFromImageRef(""))
	assert.Equal(t, "", DigestFromImageRef("docker.io/library/ubuntu:jammy"))
}

func TestDigestKey(t *testing.T) {
	key, err := DigestKey(testDigest, 4<<30)
	assert.NoError(t, err)
	assert.Equal(t, "v1-digest-sha256-5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8-4294967296", key)

	_, err = DigestKey("docker.io/library/ubuntu@"+testDigest, 4<<30)
	assert.Error(t, err)
}

func writeTree(t *testing.T) string {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "etc"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "lib/systemd"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sbin"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "etc/hostname"), []byte("ubuntu\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "lib/systemd/systemd"), []byte("#!/bin/sh\n"), 0755))
	require.NoError(t, os.Symlink("/lib/systemd/systemd", filepath.Join(root, "sbin/init")))
	mtime := time.Unix(1700000000, 0)
	tv := []unix.Timeval{unix.NsecToTimeval(mtime.UnixNano()), unix.NsecToTimeval(mtime.UnixNano())}
	require.NoError(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return unix.Lutimes(path, tv)
	}))
	return root
}

func TestTreeDigest(t *testing.T) {
	digest, err := TreeDigest(writeTree(t))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(digest, "sha256:"))

	sameRoot := writeTree(t)
	require.NoError(t, os.Chtimes(sameRoot, time.Now(), time.Now()))
	sameDigest, err := TreeDigest(sameRoot)
	require.NoError(t, err)
	assert.Equal(t, digest, sameDigest, "digest must only depend on the tree below root")

	changes := map[string]func(root string) error{
		"content": func(root string) error {
			return os.WriteFile(filepath.Join(root, "etc/hostname"), []byte("debian\n"), 0644)
		},
		"mode": func(root string) error {
			return os.Chmod(filepath.Join(root, "etc/hostname"), 0600)
		},
		"mtime": func(root string) error {
			return os.Chtimes(filepath.Join(root, "etc/hostname"), time.Now(), time.Now())
		},
		"new file": func(root string) error {
			return os.WriteFile(filepath.Join(root, "etc/motd"), nil, 0644)
		},
		"symlink target": func(root string) error {
			if err := os.Remove(filepath.Join(root, "sbin/init")); err != nil {
				return err
			}
			return os.Symlink("/bin/sh", filepath.Join(root, "sbin/init"))
		},
		"mtime of subdirectory": func(root string) error {
			return os.Chtimes(filepath.Join(root, "etc"), time.Now(), time.Now())
		},
		"xattr": func(root string) error {
			err := unix.Setxattr(filepath.Join(root, "etc/hostname"), "user.virtink", []byte("1"), 0)
			if err == unix.ENOTSUP {
				t.Skip("xattrs not supported")
			}
			return err
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			root := writeTree(t)
			require.NoError(t, change(root))
			changedDigest, err := TreeDigest(root)
			require.NoError(t, err)
			assert.NotEqual(t, digest, changedDigest)
		})
	}
}

func TestCheckInit(t *testing.T) {
	root := writeTree(t)
	assert.NoError(t, CheckInit(root, "/sbin/init"))
	assert.NoError(t, CheckInit(root, "/lib/systemd/systemd"))
	assert.Error(t, CheckInit(root, "/etc/hostname"))
	assert.Error(t, CheckInit(root, "/bin/init"))

	// Symlinks are resolved within the root.
	require.NoError(t, os.Symlink("/../../../../bin/sh", filepath.Join(root, "sbin/escape")))
	assert.Error(t, CheckInit(root, "/sbin/escape"))
}

func TestInitPathFromCmdline(t *testing.T) {
	assert.Equal(t, "/sbin/init", InitPathFromCmdline(""))
	assert.Equal(t, "/sbin/init", InitPathFromCmdline("console=ttyS0 root=/dev/vda rw"))
	assert.Equal(t, "/lib/systemd/systemd", InitPathFromCmdline("console=ttyS0 init=/lib/systemd/systemd rw"))
}

func requireTools(t *testing.T) {
	t.Setenv("PATH", os.Getenv("PATH")+":/usr/sbin:/sbin")
	for _, tool := range []string{"qemu-img", "mkfs.ext4", "debugfs"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found", tool)
		}
	}
}

func TestPrepare(t *testing.T) {
	requireTools(t)

	root := writeTree(t)
	cache := Cache{Dir: t.TempDir()}
	key, err := TreeKey(root, 64<<20)
	require.NoError(t, err)

	var wg sync.WaitGroup
	targets := make([]string, 4)
	errs := make([]error, len(targets))
	for i := range targets {
		targets[i] = filepath.Join(t.TempDir(), "rootfs.qcow2")
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = cache.Prepare(key, root, 64<<20, targets[i])
		}(i)
	}
	wg.Wait()

	basePath := filepath.Join(cache.Dir, key, "base.raw")
	for i, target := range targets {
		require.NoError(t, errs[i])
		info, err := qemuimg.Info(target)
		require.NoError(t, err)
		assert.Equal(t, basePath, info.BackingFilename)
		assert.Equal(t, int64(64<<20), info.VirtualSize)
	}

	output, err := exec.Command("debugfs", "-R", "cat /etc/hostname", basePath).Output()
	require.NoError(t, err)
	assert.Equal(t, "ubuntu\n", string(output))

	entries, err := os.ReadDir(filepath.Join(cache.Dir, ".tmp"))
	require.NoError(t, err)
	assert.Empty(t, entries)

	// An existing entry is reused rather than rebuilt.
	fi, err := os.Stat(basePath)
	require.NoError(t, err)
	require.NoError(t, cache.Prepare(key, t.TempDir(), 64<<20, filepath.Join(t.TempDir(), "rootfs.qcow2")))
	newFi, err := os.Stat(basePath)
	require.NoError(t, err)
	assert.Equal(t, fi.ModTime(), newFi.ModTime())

	// A rootfs larger than the disk fails to build and leaves no entry. The
	// data is random since mkfs.ext4 doesn't allocate blocks of zeros.
	data := make([]byte, 8<<20)
	_, err = rand.Read(data)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "big"), data, 0644))
	bigKey, err := TreeKey(root, 4<<20)
	require.NoError(t, err)
	assert.Error(t, cache.Prepare(bigKey, root, 4<<20, filepath.Join(t.TempDir(), "rootfs.qcow2")))
	assert.NoDirExists(t, filepath.Join(cache.Dir, bigKey))
}
