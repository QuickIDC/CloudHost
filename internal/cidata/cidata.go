// Package cidata 生成 cloud-init 的 NoCloud（cidata）数据源。
//
// 为什么需要它：静态 IPv6 不能在创建前通过网络下发（路由器 DHCP 关闭、
// 镜像也不知道网段），唯一可靠的办法是挂一张卷标为 CIDATA 的 ISO，
// cloud-init 在开机、无网络时就能读到 network-config 并配置静态地址。
package cidata

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/QuickIDC/HostAgent-Go/internal/model"
)

// Files 返回 cidata ISO 需要写入的三个文件。
func Files(vm *model.VMConfig) (map[string][]byte, error) {
	if vm == nil {
		return nil, fmt.Errorf("虚拟机配置为空")
	}
	meta := fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n", vm.VmUUID, sanitizeHostname(vm.VmUUID))

	userData := "#cloud-config\n"
	if hostname := sanitizeHostname(vm.VmUUID); hostname != "" {
		userData += fmt.Sprintf("hostname: %s\nmanage_etc_hosts: true\n", hostname)
	}

	network := BuildNetworkConfig(vm)
	return map[string][]byte{
		"meta-data":      []byte(meta),
		"user-data":      []byte(userData),
		"network-config": []byte(network),
	}, nil
}

// BuildNetworkConfig 生成 netplan v2 的 network-config。
//
// 只写有静态地址的网卡：IPv4 与 IPv6 都按各自前缀写进 addresses，
// 网关写 routes，DNS 合并去重。没有静态地址时返回空串（由 DHCP 处理）。
func BuildNetworkConfig(vm *model.VMConfig) string {
	if vm == nil || len(vm.NicAll) == 0 {
		return ""
	}
	names := make([]string, 0, len(vm.NicAll))
	for name := range vm.NicAll {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	wrote := false
	for _, name := range names {
		nic := vm.NicAll[name]
		var addrs []string
		var routes [][2]string
		var dns []string

		if ip4 := strings.TrimSpace(nic.IP4Addr); ip4 != "" {
			addrs = append(addrs, fmt.Sprintf("%s/%d", ip4, maskPrefix(nic.NicMask)))
			if gw := strings.TrimSpace(nic.NicGate); gw != "" {
				routes = append(routes, [2]string{"0.0.0.0/0", gw})
			}
			dns = append(dns, nic.DNSAddr...)
		}
		if ip6 := strings.TrimSpace(nic.IP6Addr); ip6 != "" {
			addrs = append(addrs, fmt.Sprintf("%s/%d", ip6, nic.IPv6PrefixLen()))
			if gw := strings.TrimSpace(nic.IP6Gate); gw != "" {
				routes = append(routes, [2]string{"::/0", gw})
			}
			dns = append(dns, nic.IP6DNS...)
		}
		if len(addrs) == 0 {
			continue
		}
		if !wrote {
			b.WriteString("version: 2\nethernets:\n")
			wrote = true
		}
		// netplan 的 match 用网卡名；主控不知道 guest 内的名字时用 mac 匹配
		if mac := strings.TrimSpace(nic.MacAddr); mac != "" {
			b.WriteString(fmt.Sprintf("  %s:\n    match:\n      macaddress: \"%s\"\n", safeKey(name), strings.ToLower(mac)))
		} else {
			b.WriteString(fmt.Sprintf("  %s:\n    match:\n      name: \"%s\"\n", safeKey(name), safeKey(name)))
		}
		b.WriteString("    dhcp4: false\n    dhcp6: false\n    addresses:\n")
		for _, a := range addrs {
			b.WriteString("      - " + a + "\n")
		}
		if len(routes) > 0 {
			b.WriteString("    routes:\n")
			for _, r := range routes {
				b.WriteString(fmt.Sprintf("      - to: %s\n        via: %s\n", r[0], r[1]))
			}
		}
		if dns = uniqueStrings(dns); len(dns) > 0 {
			b.WriteString("    nameservers:\n      addresses: [" + strings.Join(dns, ", ") + "]\n")
		}
	}
	return b.String()
}

// maskPrefix 点分掩码转前缀长度；已是数字则原样使用，非法时按 /24。
func maskPrefix(mask string) int {
	mask = strings.TrimSpace(strings.TrimPrefix(mask, "/"))
	if mask == "" {
		return 24
	}
	if isDigits(mask) {
		if n := atoi(mask); n >= 0 && n <= 32 {
			return n
		}
		return 24
	}
	ip := net.ParseIP(mask)
	if ip == nil {
		return 24
	}
	ones, bits := net.IPMask(ip.To4()).Size()
	if bits != 32 || ones < 0 {
		return 24
	}
	return ones
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

func safeKey(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "eth0"
	}
	return name
}

func sanitizeHostname(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r == '_' || r == '.' || r == ' ':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 63 {
		out = out[:63]
	}
	return out
}

func uniqueStrings(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
