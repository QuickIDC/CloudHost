package driver

import (
	"strings"
	"testing"

	"github.com/QuickIDC/HostAgent-Go/internal/model"
)

func nic(ip4, ip6 string) model.NCConfig {
	return model.NCConfig{NicName: "eth0", NicType: "pub", IP4Addr: ip4, IP6Addr: ip6}
}

// 点分十进制掩码必须换算成前缀长度，写成 /255.255.255.0 会让 systemd-networkd 拒绝整份配置。
func TestBuildCloudInitNetworkIPv4Mask(t *testing.T) {
	n := nic("10.2.20.100", "")
	n.NicMask = "255.255.255.0"
	n.NicGate = "10.2.20.1"
	n.DNSAddr = []string{"223.5.5.5"}
	cfg := &model.VMConfig{NicAll: map[string]model.NCConfig{"eth0": n}}

	out := buildCloudInitNetwork(cfg)
	if !out.Static {
		t.Fatal("有静态地址时应生成静态配置")
	}
	for _, want := range []string{"Address=10.2.20.100/24", "Gateway=10.2.20.1", "DNS=223.5.5.5"} {
		if !strings.Contains(out.Networkd, want) {
			t.Errorf("networkd 缺少 %q:\n%s", want, out.Networkd)
		}
	}
	if strings.Contains(out.Networkd, "255.255.255.0") {
		t.Errorf("掩码未转换成前缀长度:\n%s", out.Networkd)
	}
	if !strings.Contains(out.Netplan, "dhcp4: false") || !strings.Contains(out.Netplan, "- to: 0.0.0.0/0") {
		t.Errorf("netplan 未按静态 IPv4 生成:\n%s", out.Netplan)
	}
}

// 双栈：IPv4 与 IPv6 都要写进同一张网卡，IPv6 用 ip6_prefix 作掩码。
func TestBuildCloudInitNetworkDualStack(t *testing.T) {
	n := nic("10.2.20.100", "240e:3b0:1234:5600::2")
	n.NicMask = "255.255.255.0"
	n.NicGate = "10.2.20.1"
	n.IP6Prefix = 64
	n.IP6Gate = "240e:3b0:1234:5600::1"
	cfg := &model.VMConfig{NicAll: map[string]model.NCConfig{"eth0": n}}

	out := buildCloudInitNetwork(cfg)
	for _, want := range []string{
		"Address=10.2.20.100/24",
		"Address=240e:3b0:1234:5600::2/64",
		"Gateway=240e:3b0:1234:5600::1",
	} {
		if !strings.Contains(out.Networkd, want) {
			t.Errorf("networkd 缺少 %q:\n%s", want, out.Networkd)
		}
	}
	if !strings.Contains(out.Netplan, "- to: ::/0") {
		t.Errorf("netplan 缺少 IPv6 默认路由:\n%s", out.Netplan)
	}
	if !strings.Contains(out.Netplan, "- 240e:3b0:1234:5600::2/64") {
		t.Errorf("netplan 缺少 IPv6 地址:\n%s", out.Netplan)
	}
}

// 只有 IPv6 静态地址（/128 单地址）时，IPv4 仍走 DHCP，避免实例完全没 v4。
func TestBuildCloudInitNetworkIPv6Only(t *testing.T) {
	n := nic("", "240e:3b0:1234:5600::2")
	n.IP6Prefix = 128
	n.IP6Gate = "240e:3b0:1234:5600::1"
	cfg := &model.VMConfig{NicAll: map[string]model.NCConfig{"eth0": n}}

	out := buildCloudInitNetwork(cfg)
	if !strings.Contains(out.Networkd, "Address=240e:3b0:1234:5600::2/128") {
		t.Errorf("IPv6 /128 地址未写入:\n%s", out.Networkd)
	}
	if !strings.Contains(out.Netplan, "dhcp4: true") {
		t.Errorf("只有 IPv6 静态地址时 dhcp4 应保持开启:\n%s", out.Netplan)
	}
	if !strings.Contains(out.Netplan, "dhcp6: false") {
		t.Errorf("有静态 IPv6 时应关闭 dhcp6:\n%s", out.Netplan)
	}
}

// 没有任何地址时退回 DHCP。
func TestBuildCloudInitNetworkDHCPFallback(t *testing.T) {
	cfg := &model.VMConfig{NicAll: map[string]model.NCConfig{"eth0": {NicName: "eth0"}}}
	out := buildCloudInitNetwork(cfg)
	if out.Static {
		t.Fatal("无地址时不应生成静态配置")
	}
	if !strings.Contains(out.Networkd, "DHCP=yes") {
		t.Errorf("networkd 应退回 DHCP:\n%s", out.Networkd)
	}
	if !strings.Contains(out.Netplan, "dhcp4: true") || !strings.Contains(out.Netplan, "dhcp6: true") {
		t.Errorf("netplan 应退回 DHCP:\n%s", out.Netplan)
	}
}

func TestMaskToPrefix(t *testing.T) {
	cases := map[string]int{
		"":                24,
		"255.255.255.0":   24,
		"255.255.0.0":     16,
		"255.255.255.252": 30,
		"24":              24,
		"/24":             24,
		"bad":             24,
	}
	for in, want := range cases {
		if got := maskToPrefix(in); got != want {
			t.Errorf("maskToPrefix(%q) = %d，期望 %d", in, got, want)
		}
	}
}
