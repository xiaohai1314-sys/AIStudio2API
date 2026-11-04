package api

import (
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

// clientAddress 返回请求的客户端地址。只有来自可信代理的请求才读取 X-Forwarded-For，
// 并从右向左取第一个非可信代理地址，避免客户端伪造转发头。
func clientAddress(r *http.Request, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || host == "" {
		return r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.Unmap()
	if !trustedProxy(peer, trusted) {
		return peer.String()
	}
	addresses := forwardedAddresses(r.Header.Values("X-Forwarded-For"))
	for index := len(addresses) - 1; index >= 0; index-- {
		if !trustedProxy(addresses[index], trusted) {
			return addresses[index].String()
		}
	}
	return peer.String()
}

func trustedProxy(address netip.Addr, trusted []netip.Prefix) bool {
	return slices.ContainsFunc(trusted, func(prefix netip.Prefix) bool { return prefix.Contains(address) })
}

func forwardedAddresses(values []string) []netip.Addr {
	var addresses []netip.Addr
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if host, _, err := net.SplitHostPort(item); err == nil {
				item = host
			}
			address, err := netip.ParseAddr(strings.Trim(item, "[]"))
			if err == nil {
				addresses = append(addresses, address.Unmap())
			}
		}
	}
	return addresses
}
