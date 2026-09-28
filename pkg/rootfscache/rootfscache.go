// Package rootfscache manages the node-local cache of rootfs disks built from
// the images of imageRootfs volumes.
//
// Each cache entry is a raw ext4 image built from an image's filesystem, and
// is shared by the VMs on the node as the backing file of their own qcow2
// overlays. Its layout is:
//
//	<dir>/<key>/base.raw    the rootfs disk
//	<dir>/<key>/last-used   mtime is when the entry was last used by a VM
//	<dir>/locks/<key>.lock  flock(2) held while building, using or deleting it
//	<dir>/.tmp/<key>-*      entries being built
package rootfscache

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	securejoin "github.com/cyphar/filepath-securejoin"
	"golang.org/x/sys/unix"

	"github.com/smartxworks/virtink/pkg/qemuimg"
)

// DefaultDir is where the cache is stored on nodes. The same path is used in
// every container, since it appears in the overlays' backing file paths.
const DefaultDir = "/var/lib/virtink/rootfs-cache"

// keyVersion changes whenever the way entries are built changes, so that
// entries built differently are never reused.
const keyVersion = "v1"

const (
	baseFileName     = "base.raw"
	lastUsedFileName = "last-used"
	locksDirName     = "locks"
	tmpDirName       = ".tmp"
)

var digestRegexp = regexp.MustCompile(`^([a-z0-9]+):([a-f0-9]{32,})$`)

// DigestKey returns the key of the entry built from the image with the given
// content digest (e.g. "sha256:...").
func DigestKey(digest string, size int64) (string, error) {
	m := digestRegexp.FindStringSubmatch(digest)
	if m == nil {
		return "", fmt.Errorf("invalid digest %q", digest)
	}
	return fmt.Sprintf("%s-digest-%s-%s-%d", keyVersion, m[1], m[2], size), nil
}

// TreeKey returns the key of the entry built from the filesystem tree at root,
// computed from the tree's content.
func TreeKey(root string, size int64) (string, error) {
	digest, err := TreeDigest(root)
	if err != nil {
		return "", fmt.Errorf("compute tree digest: %s", err)
	}
	return fmt.Sprintf("%s-tree-%s-%d", keyVersion, strings.Replace(digest, ":", "-", 1), size), nil
}

// DigestFromReference returns the digest an image reference is pinned to, or
// "" if the reference is not pinned (e.g. it's a tag).
func DigestFromReference(reference string) string {
	i := strings.LastIndex(reference, "@")
	if i < 0 || !digestRegexp.MatchString(reference[i+1:]) {
		return ""
	}
	return reference[i+1:]
}

type Cache struct {
	Dir string
}

func (c *Cache) entryDir(key string) string {
	return filepath.Join(c.Dir, key)
}

func (c *Cache) lockPath(key string) string {
	return filepath.Join(c.Dir, locksDirName, key+".lock")
}

// lock takes the lock of the entry with the given key. It blocks unless
// nonBlocking is true, in which case ok is false if the lock is held.
func (c *Cache) lock(key string, nonBlocking bool) (unlock func(), ok bool, err error) {
	if err := os.MkdirAll(filepath.Join(c.Dir, locksDirName), 0700); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(c.lockPath(key), os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, false, err
	}
	how := unix.LOCK_EX
	if nonBlocking {
		how |= unix.LOCK_NB
	}
	if err := unix.Flock(int(f.Fd()), how); err != nil {
		f.Close()
		if nonBlocking && err == unix.EWOULDBLOCK {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("lock %s: %s", f.Name(), err)
	}
	return func() { f.Close() }, true, nil
}

// Prepare creates the VM's qcow2 disk at targetPath, backed by the cache
// entry with the given key. The entry is built from the filesystem tree at
// root if it doesn't exist yet.
func (c *Cache) Prepare(key string, root string, size int64, targetPath string) error {
	unlock, _, err := c.lock(key, false)
	if err != nil {
		return err
	}
	defer unlock()

	entryDir := c.entryDir(key)
	basePath := filepath.Join(entryDir, baseFileName)
	if _, err := os.Stat(basePath); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err := c.build(key, root, size); err != nil {
			return fmt.Errorf("build rootfs: %s", err)
		}
	}

	if err := touch(filepath.Join(entryDir, lastUsedFileName)); err != nil {
		return err
	}
	// The overlay is created while the entry is locked, so that it's either
	// seen by GC or the entry was just used.
	return qemuimg.CreateOverlay(targetPath, basePath, "raw")
}

func (c *Cache) build(key string, root string, size int64) error {
	tmpRootDir := filepath.Join(c.Dir, tmpDirName)
	if err := os.MkdirAll(tmpRootDir, 0700); err != nil {
		return err
	}
	tmpDir, err := os.MkdirTemp(tmpRootDir, key+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	basePath := filepath.Join(tmpDir, baseFileName)
	f, err := os.OpenFile(basePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	err = f.Truncate(size)
	f.Close()
	if err != nil {
		return err
	}

	cmd := exec.Command("mkfs.ext4", "-q", "-F", "-d", root, basePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%q: %s: %s", cmd.String(), err, output)
	}
	if err := syncFile(basePath); err != nil {
		return err
	}
	return os.Rename(tmpDir, c.entryDir(key))
}

func syncFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func touch(path string) error {
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return os.WriteFile(path, nil, 0600)
	}
	return nil
}

// CheckInit checks that initPath exists as an executable file in the
// filesystem tree at root, resolving symlinks within root.
func CheckInit(root string, initPath string) error {
	path, err := securejoin.SecureJoin(root, initPath)
	if err != nil {
		return err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("init %s not found in the image: %s", initPath, err)
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("init %s in the image is not an executable file", initPath)
	}
	return nil
}

// InitPathFromCmdline returns the path of init that the kernel runs given its
// command line.
func InitPathFromCmdline(cmdline string) string {
	initPath := "/sbin/init"
	for _, field := range strings.Fields(cmdline) {
		if v, ok := strings.CutPrefix(field, "init="); ok {
			initPath = v
		}
	}
	return initPath
}
