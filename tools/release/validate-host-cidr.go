// Command validate-host-cidr проверяет exact host CIDR для offline release
// render без обращения к Kubernetes API.
package main

import (
	"fmt"
	"net"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		_, _ = fmt.Fprintln(os.Stderr, "usage: validate-host-cidr <ipv4/32|ipv6/128>")
		os.Exit(64)
	}
	ip, network, err := net.ParseCIDR(os.Args[1])
	if err != nil || ip == nil || network == nil || !ip.Equal(network.IP) {
		_, _ = fmt.Fprintln(os.Stderr, "host CIDR is invalid")
		os.Exit(64)
	}
	ones, bits := network.Mask.Size()
	if ones != bits || bits != 32 && bits != 128 {
		_, _ = fmt.Fprintln(os.Stderr, "host CIDR must contain exactly one address")
		os.Exit(64)
	}
}
