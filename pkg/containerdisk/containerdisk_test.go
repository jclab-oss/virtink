package containerdisk

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smartxworks/virtink/pkg/qemuimg"
)

func TestFindDiskFile(t *testing.T) {
	imageDir := t.TempDir()
	_, err := FindDiskFile(imageDir)
	assert.Error(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(imageDir, "disk"), nil, 0644))
	diskFile, err := FindDiskFile(imageDir)
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(imageDir, "disk"), diskFile)

	imageDir = t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(imageDir, "disk"), 0755))
	_, err = FindDiskFile(imageDir)
	assert.Error(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(imageDir, "disk", "ubuntu.qcow2"), nil, 0644))
	diskFile, err = FindDiskFile(imageDir)
	assert.NoError(t, err)
	assert.Equal(t, filepath.Join(imageDir, "disk", "ubuntu.qcow2"), diskFile)

	require.NoError(t, os.WriteFile(filepath.Join(imageDir, "disk", "another.img"), nil, 0644))
	_, err = FindDiskFile(imageDir)
	assert.Error(t, err)
}

func TestPrepare(t *testing.T) {
	if _, err := exec.LookPath("qemu-img"); err != nil {
		t.Skip("qemu-img not found")
	}

	for _, format := range []string{"raw", "qcow2"} {
		t.Run(format, func(t *testing.T) {
			imageDir := t.TempDir()
			diskPath := filepath.Join(imageDir, "disk")
			require.NoError(t, exec.Command("qemu-img", "create", "-q", "-f", format, diskPath, "64M").Run())

			targetPath := filepath.Join(t.TempDir(), "disk.qcow2")
			require.NoError(t, Prepare(imageDir, targetPath))

			info, err := qemuimg.Info(targetPath)
			require.NoError(t, err)
			assert.Equal(t, "qcow2", info.Format)
			assert.Equal(t, diskPath, info.BackingFilename)
			assert.Equal(t, int64(64<<20), info.VirtualSize)
		})
	}

	t.Run("other formats", func(t *testing.T) {
		imageDir := t.TempDir()
		require.NoError(t, exec.Command("qemu-img", "create", "-q", "-f", "vmdk", filepath.Join(imageDir, "disk"), "64M").Run())

		targetPath := filepath.Join(t.TempDir(), "disk.qcow2")
		require.NoError(t, Prepare(imageDir, targetPath))

		info, err := qemuimg.Info(targetPath)
		require.NoError(t, err)
		assert.Equal(t, "qcow2", info.Format)
		assert.Empty(t, info.ExternalFile())
	})

	t.Run("external files", func(t *testing.T) {
		secretPath := filepath.Join(t.TempDir(), "secret")
		require.NoError(t, os.WriteFile(secretPath, []byte("secret"), 0600))

		imageDir := t.TempDir()
		require.NoError(t, exec.Command("qemu-img", "create", "-q", "-f", "qcow2", "-F", "raw", "-b", secretPath, filepath.Join(imageDir, "disk"), "64M").Run())
		assert.ErrorContains(t, Prepare(imageDir, filepath.Join(t.TempDir(), "disk.qcow2")), "must not reference external file")

		imageDir = t.TempDir()
		require.NoError(t, exec.Command("qemu-img", "create", "-q", "-f", "qcow2", "-o", "data_file="+secretPath+".data", filepath.Join(imageDir, "disk"), "64M").Run())
		assert.ErrorContains(t, Prepare(imageDir, filepath.Join(t.TempDir(), "disk.qcow2")), "must not reference external file")
	})
}
