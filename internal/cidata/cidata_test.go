package cidata

import (
	"strings"
	"testing"

	"github.com/QuickIDC/HostAgent-Go/internal/model"
)

func dualStackVM() *model.VMConfig {
	return &model.VMConfig{
		VmUUID: "vm-1",
		NicAll: map[string]model.NCConfig{
			"eth0": {
				NicName: "eth0", MacAddr: "AA:BB:CC:DD:EE:FF",
				IP4Addr: "10.2.20.100", NicMask: "255.255.255.0", NicGate: "10.2.20.1",
				DNSAddr: []string{"223.5.5.5"},
				IP6Addr: "240e:3b0:1234:5600::2", IP6Prefix: 128,
				IP6Gate: "240e:3b0:1234:5600::1",
				IP6DNS:  []string{"2400:3200::1"},
			},
		},
	}
}

// 双栈 + /128：network-config 必须同时写出 v4/v6 地址、路由和两套 DNS，
// 并且用 MAC 匹配网卡（此时还不知道 guest 内的接口名）。
func TestBuildNetworkConfigDualStack(t *testing.T) {
	cfg := BuildNetworkConfig(dualStackVM())
	for _, want := range []string{
		"version: 2",
		"macaddress: \"aa:bb:cc:dd:ee:ff\"",
		"dhcp4: false",
		"dhcp6: false",
		"- 10.2.20.100/24",
		"- 240e:3b0:1234:5600::2/128",
		"to: 0.0.0.0/0",
		"via: 10.2.20.1",
		"to: ::/0",
		"via: 240e:3b0:1234:5600::1",
		"223.5.5.5",
		"2400:3200::1",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("network-config 缺少 %q:\n%s", want, cfg)
		}
	}
}

// 只有 IPv6 静态地址时，不应写出 dhcp4: false 之外的 v4 配置。
func TestBuildNetworkConfigIPv6Only(t *testing.T) {
	vm := dualStackVM()
	nic := vm.NicAll["eth0"]
	nic.IP4Addr, nic.NicGate, nic.DNSAddr = "", "", nil
	vm.NicAll["eth0"] = nic

	cfg := BuildNetworkConfig(vm)
	if strings.Contains(cfg, "0.0.0.0/0") {
		t.Errorf("没有 IPv4 地址时不应写 v4 默认路由:\n%s", cfg)
	}
	if !strings.Contains(cfg, "240e:3b0:1234:5600::2/128") {
		t.Errorf("IPv6 地址缺失:\n%s", cfg)
	}
}

// 没有静态地址时不生成 ethernets（交给 DHCP）。
func TestBuildNetworkConfigEmpty(t *testing.T) {
	vm := &model.VMConfig{VmUUID: "vm-1", NicAll: map[string]model.NCConfig{
		"eth0": {NicName: "eth0", MacAddr: "aa:bb:cc:dd:ee:ff"},
	}}
	if cfg := BuildNetworkConfig(vm); cfg != "" {
		t.Errorf("无静态地址时应返回空配置，实际:\n%s", cfg)
	}
}

func TestFilesIncludeAllThree(t *testing.T) {
	files, err := Files(dualStackVM())
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	for _, name := range []string{"meta-data", "user-data", "network-config"} {
		if _, ok := files[name]; !ok {
			t.Errorf("缺少 %s", name)
		}
	}
	if !strings.Contains(string(files["meta-data"]), "instance-id: vm-1") {
		t.Errorf("meta-data 缺少 instance-id:\n%s", files["meta-data"])
	}
	if !strings.Contains(string(files["network-config"]), "240e:3b0:1234:5600::2/128") {
		t.Errorf("network-config 未包含 IPv6 地址")
	}
}

func TestMaskPrefix(t *testing.T) {
	cases := map[string]int{
		"255.255.255.0": 24, "255.255.0.0": 16, "24": 24, "/24": 24, "": 24, "bad": 24,
	}
	for in, want := range cases {
		if got := maskPrefix(in); got != want {
			t.Errorf("maskPrefix(%q)=%d，期望 %d", in, got, want)
		}
	}
}
