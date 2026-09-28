package main

import (
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

// ipv4DevconfARPIgnore is IPV4_DEVCONF_ARP_IGNORE in linux/ip.h, the ID of the
// arp_ignore setting in IFLA_INET_CONF.
const ipv4DevconfARPIgnore = 19

// setARPIgnore sets net.ipv4.conf.<link>.arp_ignore. It's set with netlink
// rather than sysctl, since /proc/sys is read-only in unprivileged containers,
// and only needs CAP_NET_ADMIN.
func setARPIgnore(link netlink.Link, value uint32) error {
	req := nl.NewNetlinkRequest(unix.RTM_SETLINK, unix.NLM_F_ACK)
	msg := nl.NewIfInfomsg(unix.AF_UNSPEC)
	msg.Index = int32(link.Attrs().Index)
	req.AddData(msg)

	afSpec := nl.NewRtAttr(unix.IFLA_AF_SPEC, nil)
	inetConf := afSpec.AddRtAttr(unix.AF_INET, nil).AddRtAttr(unix.IFLA_INET_CONF, nil)
	inetConf.AddRtAttr(ipv4DevconfARPIgnore, nl.Uint32Attr(value))
	req.AddData(afSpec)

	_, err := req.Execute(unix.NETLINK_ROUTE, 0)
	return err
}
