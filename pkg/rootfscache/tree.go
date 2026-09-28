package rootfscache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// TreeDigest returns a digest of everything mkfs.ext4 copies from the
// filesystem tree at root: paths, file types, permissions, ownership, mtimes,
// xattrs, symlink targets, device numbers and file contents.
//
// Entries are visited in lexical order, so the digest doesn't depend on the
// order of directory entries on disk. The attributes of root itself are not
// part of the digest.
func TreeDigest(root string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			// The root is the mount point of the image volume, whose attributes
			// (e.g. mtime) come from the mount rather than the image.
			return nil
		}

		var st unix.Stat_t
		if err := unix.Lstat(path, &st); err != nil {
			return &fs.PathError{Op: "lstat", Path: path, Err: err}
		}
		fmt.Fprintf(h, "%q mode=%o uid=%d gid=%d mtime=%d", relPath, st.Mode, st.Uid, st.Gid, st.Mtim.Nano())

		switch st.Mode & syscall.S_IFMT {
		case syscall.S_IFREG:
			fmt.Fprintf(h, " size=%d content=", st.Size)
			if err := hashFileContent(h, path); err != nil {
				return err
			}
		case syscall.S_IFLNK:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, " target=%q", target)
		case syscall.S_IFCHR, syscall.S_IFBLK:
			fmt.Fprintf(h, " rdev=%d", st.Rdev)
		}

		xattrs, err := readXattrs(path)
		if err != nil {
			return err
		}
		for _, xattr := range xattrs {
			fmt.Fprintf(h, " xattr=%q", xattr)
		}
		fmt.Fprintln(h)
		return nil
	})
	if err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func hashFileContent(h hash.Hash, path string) error {
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	fh := sha256.New()
	if _, err := io.Copy(fh, f); err != nil {
		return err
	}
	_, err = io.WriteString(h, hex.EncodeToString(fh.Sum(nil)))
	return err
}

// readXattrs returns the sorted "name=value" xattrs of path.
func readXattrs(path string) ([]string, error) {
	names, err := listXattrs(path)
	if err != nil {
		return nil, err
	}

	var xattrs []string
	for _, name := range names {
		value, err := getXattr(path, name)
		if err != nil {
			if err == unix.ENODATA {
				continue
			}
			return nil, &fs.PathError{Op: "lgetxattr " + name, Path: path, Err: err}
		}
		xattrs = append(xattrs, name+"="+hex.EncodeToString(value))
	}
	sort.Strings(xattrs)
	return xattrs, nil
}

func listXattrs(path string) ([]string, error) {
	for {
		size, err := unix.Llistxattr(path, nil)
		if err == unix.ENOTSUP {
			return nil, nil
		}
		if err != nil {
			return nil, &fs.PathError{Op: "llistxattr", Path: path, Err: err}
		}
		if size == 0 {
			return nil, nil
		}
		buf := make([]byte, size)
		size, err = unix.Llistxattr(path, buf)
		if err == unix.ERANGE {
			// The xattrs changed between the two calls.
			continue
		}
		if err != nil {
			return nil, &fs.PathError{Op: "llistxattr", Path: path, Err: err}
		}
		// The size returned without a buffer is only an upper bound, e.g. on
		// overlayfs, which hides its private xattrs.
		var names []string
		for _, name := range strings.Split(string(buf[:size]), "\x00") {
			if name != "" {
				names = append(names, name)
			}
		}
		return names, nil
	}
}

func getXattr(path string, name string) ([]byte, error) {
	for {
		size, err := unix.Lgetxattr(path, name, nil)
		if err != nil {
			return nil, err
		}
		buf := make([]byte, size)
		size, err = unix.Lgetxattr(path, name, buf)
		if err == unix.ERANGE {
			continue
		}
		if err != nil {
			return nil, err
		}
		return buf[:size], nil
	}
}
