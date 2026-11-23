package driver

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/QuickIDC/HostAgent-Go/internal/model"
)

// cloudInitNetwork 注入给云镜像的静态网络配置。
// Networkd 写进 systemd-networkd 的 .network 文件，Netplan 作为 cloud-init 的
// network-config 下发；两套都给，是因为部分镜像没有 netplan 渲染器。
type cloudInitNetwork struct {
	Networkd string
	Netplan  string
	Static   bool
}

// buildCloudInitNetwork 依据网卡上的静态 IPv4 / IPv6 生成网络配置。
//
// 两个协议都没有地址时退回 DHCP，保证镜像至少能拿到地址；有地址时按静态配置，
// 这样实例开机后就是主控分配的地址，不依赖路由器的 DHCP/DHCPv6。
func buildCloudInitNetwork(cfg *model.VMConfig) cloudInitNetwork {
	v4, v6 := pickStaticNics(cfg)

	var addrs []string
	var routes [][2]string
	var dns []string

	if v4 != nil {
		prefix := maskToPrefix(v4.NicMask)
		addrs = append(addrs, fmt.Sprintf("%s/%d", strings.TrimSpace(v4.IP4Addr), prefix))
		if gw := strings.TrimSpace(v4.NicGate); gw != "" {
			routes = append(routes, [2]string{"0.0.0.0/0", gw})
		}
		dns = append(dns, v4.DNSAddr...)
	}
	if v6 != nil {
		addrs = append(addrs, fmt.Sprintf("%s/%d", strings.TrimSpace(v6.IP6Addr), v6.IPv6PrefixLen()))
		if gw := strings.TrimSpace(v6.IP6Gate); gw != "" {
			routes = append(routes, [2]string{"::/0", gw})
		}
		if len(dns) == 0 {
			dns = append(dns, v6.DNSAddr...)
		}
	}

	out := cloudInitNetwork{}
	if len(addrs) == 0 {
		out.Networkd = "[Match]\nName=en*\n[Network]\nDHCP=yes\n"
		out.Netplan = netplanBase(true, true)
		return out
	}

	out.Static = true

	var netd strings.Builder
	netd.WriteString("[Match]\nName=en*\n[Network]\n")
	for _, a := range addrs {
		netd.WriteString("Address=" + a + "\n")
	}
	for _, r := range routes {
		// systemd-networkd 支持按协议各写一条 Gateway
		netd.WriteString("Gateway=" + r[1] + "\n")
	}
	seen := map[string]bool{}
	for _, d := range dns {
		d = strings.TrimSpace(d)
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		netd.WriteString("DNS=" + d + "\n")
	}
	out.Networkd = netd.String()

	// 有静态 IPv4 才关 dhcp4；只有 IPv6 静态时 IPv4 仍交给 DHCP。
	var plan strings.Builder
	plan.WriteString("version: 2\nethernets:\n  primary:\n    match:\n      name: \"en*\"\n")
	plan.WriteString(fmt.Sprintf("    dhcp4: %t\n", v4 == nil))
	plan.WriteString("    dhcp6: false\n")
	plan.WriteString("    addresses:\n")
	for _, a := range addrs {
		plan.WriteString("      - " + a + "\n")
	}
	if len(routes) > 0 {
		plan.WriteString("    routes:\n")
		for _, r := range routes {
			plan.WriteString("      - to: " + r[0] + "\n")
			plan.WriteString("        via: " + r[1] + "\n")
		}
	}
	if len(dns) > 0 {
		plan.WriteString("    nameservers:\n      addresses: [" + strings.Join(uniqueStrings(dns), ", ") + "]\n")
	}
	out.Netplan = plan.String()
	return out
}

func netplanBase(dhcp4, dhcp6 bool) string {
	return fmt.Sprintf("version: 2\nethernets:\n  primary:\n    match:\n      name: \"en*\"\n"+
		"    dhcp4: %t\n    dhcp6: %t\n", dhcp4, dhcp6)
}

// pickStaticNics 取第一张带静态 IPv4 的网卡和第一张带静态 IPv6 的网卡。
// 按网卡名排序，保证多次生成结果稳定。
func pickStaticNics(cfg *model.VMConfig) (v4, v6 *model.NCConfig) {
	if cfg == nil || len(cfg.NicAll) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(cfg.NicAll))
	for name := range cfg.NicAll {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		nic := cfg.NicAll[name]
		if v4 == nil && strings.TrimSpace(nic.IP4Addr) != "" {
			c := nic
			v4 = &c
		}
		if v6 == nil && strings.TrimSpace(nic.IP6Addr) != "" {
			c := nic
			v6 = &c
		}
	}
	return v4, v6
}

// firstNICWithIPv6 返回第一个带静态 IPv6 的网卡，供各驱动下发静态地址。
func firstNICWithIPv6(cfg *model.VMConfig) (model.NCConfig, bool) {
	if cfg == nil {
		return model.NCConfig{}, false
	}
	names := make([]string, 0, len(cfg.NicAll))
	for name := range cfg.NicAll {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if nic := cfg.NicAll[name]; strings.TrimSpace(nic.IP6Addr) != "" {
			return nic, true
		}
	}
	return model.NCConfig{}, false
}

// maskToPrefix 把点分十进制掩码转成前缀长度；传入的已经是数字（如 "24"）
// 或解析失败时按 /24 处理，非法掩码返回 24 而不是 0，避免写出 0.0.0.0/0。
func maskToPrefix(mask string) int {
	mask = strings.TrimSpace(mask)
	if mask == "" {
		return 24
	}
	if n, err := strconv.Atoi(strings.TrimPrefix(mask, "/")); err == nil {
		if n >= 0 && n <= 32 {
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
