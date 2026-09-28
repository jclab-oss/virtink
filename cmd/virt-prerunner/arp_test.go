package main

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// TestSetARPIgnore runs in a new network namespace, which needs root or a user
// namespace, e.g. `unshare -r go test ./cmd/virt-prerunner`.
func TestSetARPIgnore(t *testing.T) {
	runtime.LockOSThread()
	// The thread is left in the new network namespace, so it's not unlocked
	// and exits with the test.
	if err := unix.Unshare(unix.CLONE_NEWNET); err != nil {
		t.Skipf("create network namespace: %s", err)
	}

	bridge := &netlink.Bridge{LinkAttrs: netlink.LinkAttrs{Name: "br-test"}}
	if err := netlink.LinkAdd(bridge); err != nil {
		t.Fatal(err)
	}
	link, err := netlink.LinkByName("br-test")
	if err != nil {
		t.Fatal(err)
	}

	if err := setARPIgnore(link, 1); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile("/proc/sys/net/ipv4/conf/br-test/arp_ignore")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "1" {
		t.Errorf("arp_ignore = %s, want 1", got)
	}
}
