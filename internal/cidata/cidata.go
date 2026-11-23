// Package cidata 生成实例的"开机自带配置"ISO。
//
// 介质里只有我们自己定义的 hostagent-net.json，由客户插件（CloudInit）开机读取，
// 不使用 cloud-init 的任何文件：地址不能靠 DHCP 拿（路由器关掉了 DHCP），
// 也不能预置在镜像里（网段由主控分配），只能通过这块只读介质带进去。
package cidata

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/QuickIDC/HostAgent-Go/internal/model"
)

// VolumeLabel 是介质的卷标，客户插件按它找盘。
const VolumeLabel = "QIDCCFG"

// FileNameHostagentNet 是客户插件读取的配置文件。
const FileNameHostagentNet = "hostagent-net.json"

// AgentNIC 是单个网卡的静态配置，字段名与插件侧结构保持一致。
type AgentNIC struct {
	MAC           string   `json:"mac"`
	IP            string   `json:"ip"`
	Prefix        int      `json:"prefix"`
	Gate          string   `json:"gate,omitempty"`
	DNS           []string `json:"dns,omitempty"`
	DisableDHCPv6 bool     `json:"disable_dhcpv6"`
}

// Files 返回要写进介质的文件，只有自研配置，不含 cloud-init 文件。
func Files(vm *model.VMConfig) (map[string][]byte, error) {
	cfg, err := BuildAgentConfig(vm)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{FileNameHostagentNet: cfg}, nil
}

// BuildAgentConfig 生成 hostagent-net.json：
// 只包含分配方式为 static 且已有地址的网卡，IPv4 / IPv6 都支持。
func BuildAgentConfig(vm *model.VMConfig) ([]byte, error) {
	if vm == nil {
		return nil, fmt.Errorf("虚拟机配置为空")
	}
	names := make([]string, 0, len(vm.NicAll))
	for name := range vm.NicAll {
		names = append(names, name)
	}
	sort.Strings(names)

	nics := make([]AgentNIC, 0, len(names))
	for _, name := range names {
		nic := vm.NicAll[name]
		mac := strings.ToLower(strings.TrimSpace(nic.MacAddr))
		if mac == "" || nic.IPAssign != "static" {
			continue
		}
		if ip4 := strings.TrimSpace(nic.IP4Addr); ip4 != "" {
			nics = append(nics, AgentNIC{
				MAC:    mac,
				IP:     ip4,
				Prefix: maskPrefix(nic.NicMask),
				Gate:   strings.TrimSpace(nic.NicGate),
				DNS:    uniqueStrings(nic.DNSAddr),
			})
		}
		if ip6 := strings.TrimSpace(nic.IP6Addr); ip6 != "" {
			nics = append(nics, AgentNIC{
				MAC:           mac,
				IP:            ip6,
				Prefix:        nic.IPv6PrefixLen(),
				Gate:          strings.TrimSpace(nic.IP6Gate),
				DNS:           uniqueStrings(nic.IP6DNS),
				DisableDHCPv6: true,
			})
		}
	}

	out, err := json.MarshalIndent(map[string]interface{}{"nics": nics}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("生成 %s 失败: %w", FileNameHostagentNet, err)
	}
	return out, nil
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
