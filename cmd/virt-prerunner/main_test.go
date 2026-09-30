package main

import "testing"

func TestIsVirtioConsole(t *testing.T) {
	for _, tc := range []struct {
		cmdline string
		want    bool
	}{
		{"console=hvc0 root=/dev/vda rw", true},
		{"console=ttyS0 root=/dev/vda rw", false},
		{"console=ttyS0,115200 console=hvc0 root=/dev/vda", true},
		{"console=hvc0 console=ttyAMA0 root=/dev/vda", false},
		{"root=/dev/vda rw", false},
	} {
		if got := isVirtioConsole(tc.cmdline); got != tc.want {
			t.Errorf("isVirtioConsole(%q) = %v, want %v", tc.cmdline, got, tc.want)
		}
	}
}
