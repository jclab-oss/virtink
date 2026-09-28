package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckImageMounted(t *testing.T) {
	emptyDir := t.TempDir()
	if err := checkImageMounted(emptyDir); err == nil || !strings.Contains(err.Error(), "may not support image volumes") {
		t.Errorf("empty dir: got %v, want an image volume support error", err)
	}

	imageDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(imageDir, "sbin"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := checkImageMounted(imageDir); err != nil {
		t.Errorf("non-empty dir: got %v, want nil", err)
	}

	if err := checkImageMounted(filepath.Join(imageDir, "missing")); err == nil {
		t.Error("missing dir: got nil, want an error")
	}
}
