package cidata

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuickIDC/HostAgent-Go/internal/model"
)

func staticVM() *model.VMConfig {
	return &model.VMConfig{
		VmUUID: "vm-1",
		NicAll: map[string]model.NCConfig{
			"eth0": {
				NicName: "eth0", MacAddr: "AA:BB:CC:DD:EE:FF",
				IP4Addr: "10.2.20.100", NicMask: "255.255.255.0", NicGate: "10.2.20.1",
				DNSAddr:  []string{"223.5.5.5"},
				IP6Addr:  "240e:3b0:1234:5600::2", IP6Prefix: 128,
				IP6Gate: "240e:3b0:1234:5600::1",
				IP6DNS:  []string{"2400:3200::1"},
				IPAssign: "static",
			},
		},
	}
}

// 介质里只能有我们自己的配置，不能出现 cloud-init 的 meta-data/user-data/network-config。
func TestFilesOnlyOurConfig(t *testing.T) {
	files, err := Files(staticVM())
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("应只有 1 个文件，实际 %d: %v", len(files), keys(files))
	}
	if _, ok := files[FileNameHostagentNet]; !ok {
		t.Errorf("缺少 %s", FileNameHostagentNet)
	}
	for _, forbidden := range []string{"meta-data", "user-data", "network-config"} {
		if _, ok := files[forbidden]; ok {
			t.Errorf("不应包含 cloud-init 文件 %s", forbidden)
		}
	}
}

// 客户插件读的 JSON：字段名固定，双栈各一条，网关/DNS/前缀齐全。
func TestBuildAgentConfigDualStack(t *testing.T) {
	raw, err := BuildAgentConfig(staticVM())
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	var parsed struct {
		NICs []AgentNIC `json:"nics"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("JSON 解析失败: %v\n%s", err, raw)
	}
	if len(parsed.NICs) != 2 {
		t.Fatalf("双栈应生成 2 条，实际 %d:\n%s", len(parsed.NICs), raw)
	}
	var v4, v6 bool
	for _, n := range parsed.NICs {
		switch n.IP {
		case "10.2.20.100":
			v4 = true
			if n.Prefix != 24 || n.Gate != "10.2.20.1" || len(n.DNS) != 1 {
				t.Errorf("IPv4 字段不完整: %+v", n)
			}
		case "240e:3b0:1234:5600::2":
			v6 = true
			if n.Prefix != 128 || n.Gate != "240e:3b0:1234:5600::1" || !n.DisableDHCPv6 {
				t.Errorf("IPv6 字段不完整: %+v", n)
			}
			if len(n.DNS) != 1 || n.DNS[0] != "2400:3200::1" {
				t.Errorf("IPv6 DNS 未写入: %v", n.DNS)
			}
		}
	}
	if !v4 || !v6 {
		t.Errorf("缺少 v4 或 v6 配置: v4=%v v6=%v\n%s", v4, v6, raw)
	}
}

// dhcp 模式不写配置：地址由爱快下发。
func TestBuildAgentConfigSkipsDHCP(t *testing.T) {
	vm := staticVM()
	nic := vm.NicAll["eth0"]
	nic.IPAssign = "dhcp"
	vm.NicAll["eth0"] = nic

	raw, err := BuildAgentConfig(vm)
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if strings.Contains(string(raw), "240e:3b0:1234:5600::2") {
		t.Errorf("dhcp 模式不应包含地址:\n%s", raw)
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

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
