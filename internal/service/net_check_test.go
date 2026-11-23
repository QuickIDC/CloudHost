package service

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuickIDC/HostAgent-Go/internal/data"
	"github.com/QuickIDC/HostAgent-Go/internal/model"
)

// newTestIPAllocService 构造带临时库的 IP 分配服务。
// poolsJSON 为主机 ipaddr_maps 的 JSON 文本。
func newTestIPAllocService(t *testing.T, poolsJSON string) *IPAllocService {
	t.Helper()
	db, err := data.NewDB(t.TempDir())
	if err != nil {
		t.Fatalf("创建临时数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.CreateHost(&data.HSConfigRow{
		HsName:     "h1",
		ServerType: "PromoxSetup",
		IPAddrMaps: poolsJSON,
		IPAddrDDNS: `["119.29.29.29","223.5.5.5"]`,
	}); err != nil {
		t.Fatalf("写入主机配置失败: %v", err)
	}
	return NewIPAllocService(db)
}

const testNatPool = `{"nat":{"from":"10.2.20.100","nums":5,"type":"nat","vers":"ipv4",` +
	`"gate":"10.2.20.1","mask":"255.255.255.0"}}`

// 新增网卡（ip4_addr 为空）必须从地址池拿到 IP、网关、掩码、DNS 与 MAC，
// 否则后续的爱快静态绑定会因缺少 IP/MAC 被整体跳过。
func TestNetCheckAllocatesMissingIP(t *testing.T) {
	svc := newTestIPAllocService(t, testNatPool)

	vm := &model.VMConfig{
		VmUUID: "vm-1",
		NicAll: map[string]model.NCConfig{
			"ethernet0": {NicName: "ethernet0", NicType: "nat"},
		},
	}
	if err := svc.NetCheck("h1", vm); err != nil {
		t.Fatalf("分配失败: %v", err)
	}

	nic := vm.NicAll["ethernet0"]
	if nic.IP4Addr != "10.2.20.100" {
		t.Errorf("应分配到池首地址 10.2.20.100，实际 %q", nic.IP4Addr)
	}
	if nic.NicGate != "10.2.20.1" {
		t.Errorf("网关应为 10.2.20.1，实际 %q", nic.NicGate)
	}
	if nic.NicMask != "255.255.255.0" {
		t.Errorf("掩码应为 255.255.255.0，实际 %q", nic.NicMask)
	}
	if len(nic.DNSAddr) != 2 {
		t.Errorf("应回填主机 DNS，实际 %v", nic.DNSAddr)
	}
	if nic.MacAddr == "" {
		t.Error("MAC 应按 IP 推导生成")
	}
}

// 地址池必须避开其它虚拟机已占用的地址。
func TestNetCheckSkipsAllocatedIP(t *testing.T) {
	svc := newTestIPAllocService(t, testNatPool)

	occupied := &model.VMConfig{
		VmUUID: "vm-old",
		NicAll: map[string]model.NCConfig{
			"ethernet0": {NicName: "ethernet0", IP4Addr: "10.2.20.100"},
		},
	}
	if err := svc.db.UpsertVMConfig("h1", "vm-old", occupied); err != nil {
		t.Fatalf("写入已占用虚拟机失败: %v", err)
	}

	vm := &model.VMConfig{
		VmUUID: "vm-new",
		NicAll: map[string]model.NCConfig{
			"ethernet0": {NicName: "ethernet0", NicType: "nat"},
		},
	}
	if err := svc.NetCheck("h1", vm); err != nil {
		t.Fatalf("分配失败: %v", err)
	}
	if got := vm.NicAll["ethernet0"].IP4Addr; got != "10.2.20.101" {
		t.Errorf("应跳过已占用的 .100，实际 %q", got)
	}
}

// 已有 IP 的网卡保持原地址（不能因为再次保存而被改写）。
func TestNetCheckKeepsExistingIP(t *testing.T) {
	svc := newTestIPAllocService(t, testNatPool)

	vm := &model.VMConfig{
		VmUUID: "vm-1",
		NicAll: map[string]model.NCConfig{
			"ethernet0": {NicName: "ethernet0", IP4Addr: "10.2.20.200", MacAddr: "aa:bb:cc:dd:ee:ff"},
		},
	}
	if err := svc.NetCheck("h1", vm); err != nil {
		t.Fatalf("检查失败: %v", err)
	}
	if got := vm.NicAll["ethernet0"].IP4Addr; got != "10.2.20.200" {
		t.Errorf("已有地址不应被改写，实际 %q", got)
	}
}

// 双栈池：同一张网卡同时拿到 IPv4 与 IPv6，IPv6 前缀长度写入 ip6_prefix。
func TestNetCheckAllocatesDualStack(t *testing.T) {
	const pools = `{"nat":{"from":"10.2.20.100","nums":5,"type":"nat","vers":"ipv4",` +
		`"gate":"10.2.20.1","mask":"255.255.255.0"},` +
		`"v6":{"from":"2408:8207:1234:5600::100","nums":10,"type":"nat","vers":"ipv6",` +
		`"gate":"fe80::1","prefix":64}}`
	svc := newTestIPAllocService(t, pools)

	vm := &model.VMConfig{
		VmUUID: "vm-1",
		NicAll: map[string]model.NCConfig{
			"ethernet0": {NicName: "ethernet0", NicType: "nat"},
		},
	}
	if err := svc.NetCheck("h1", vm); err != nil {
		t.Fatalf("分配失败: %v", err)
	}

	nic := vm.NicAll["ethernet0"]
	if nic.IP4Addr != "10.2.20.100" {
		t.Errorf("IPv4 应为池首地址，实际 %q", nic.IP4Addr)
	}
	if nic.IP6Addr != "2408:8207:1234:5600::100" {
		t.Errorf("IPv6 应为池首地址，实际 %q", nic.IP6Addr)
	}
	if nic.IP6Prefix != 64 {
		t.Errorf("IPv6 前缀长度应为 64，实际 %d", nic.IP6Prefix)
	}
	// 爱快的 DHCPv6 静态绑定只认后缀，必须与分配到的地址一致
	if got := nic.IPv6Suffix(); got != "::100" {
		t.Errorf("IPv6 后缀应为 ::100，实际 %q", got)
	}
	if nic.MacAddr == "" {
		t.Error("应按地址推导 MAC")
	}
}

// 纯 IPv6 主机（只有 ipv6 池）不应因为缺少 IPv4 池而无法创建虚拟机。
func TestNetCheckIPv6Only(t *testing.T) {
	const pools = `{"v6":{"from":"2408:8207:1234:5600::100","nums":10,"type":"nat","vers":"ipv6",` +
		`"mask":"64"}}`
	svc := newTestIPAllocService(t, pools)

	vm := &model.VMConfig{
		VmUUID: "vm-1",
		NicAll: map[string]model.NCConfig{
			"ethernet0": {NicName: "ethernet0", NicType: "nat"},
		},
	}
	if err := svc.NetCheck("h1", vm); err != nil {
		t.Fatalf("纯 IPv6 主机不应报错: %v", err)
	}

	nic := vm.NicAll["ethernet0"]
	if nic.IP4Addr != "" {
		t.Errorf("没有 IPv4 池时不应分配 IPv4，实际 %q", nic.IP4Addr)
	}
	if nic.IP6Addr != "2408:8207:1234:5600::100" {
		t.Errorf("IPv6 应为池首地址，实际 %q", nic.IP6Addr)
	}
	// mask 写前缀长度时必须能识别
	if nic.IP6Prefix != 64 {
		t.Errorf("mask=64 应解析为前缀长度 64，实际 %d", nic.IP6Prefix)
	}
	if nic.MacAddr == "" {
		t.Fatal("只有 IPv6 时也必须推导出 MAC")
	}

	// 再次执行不能因为「已有地址」而改写 MAC（爱快绑定依赖稳定的 MAC）
	first := nic.MacAddr
	if err := svc.NetCheck("h1", vm); err != nil {
		t.Fatalf("二次检查失败: %v", err)
	}
	if got := vm.NicAll["ethernet0"].MacAddr; got != first {
		t.Errorf("MAC 应保持稳定：%q → %q", first, got)
	}
}

// 地址池耗尽时必须报错（而不是静默留空，让用户以为分配成功）。
func TestNetCheckPoolExhausted(t *testing.T) {
	svc := newTestIPAllocService(t,
		`{"nat":{"from":"10.2.20.100","nums":1,"type":"nat","vers":"ipv4"}}`)

	occupied := &model.VMConfig{
		VmUUID: "vm-old",
		NicAll: map[string]model.NCConfig{
			"ethernet0": {NicName: "ethernet0", IP4Addr: "10.2.20.100"},
		},
	}
	if err := svc.db.UpsertVMConfig("h1", "vm-old", occupied); err != nil {
		t.Fatalf("写入已占用虚拟机失败: %v", err)
	}

	vm := &model.VMConfig{
		VmUUID: "vm-new",
		NicAll: map[string]model.NCConfig{
			"ethernet0": {NicName: "ethernet0", NicType: "nat"},
		},
	}
	err := svc.NetCheck("h1", vm)
	if err == nil {
		t.Fatal("地址池耗尽时应返回错误")
	}
	if !strings.Contains(err.Error(), "地址池") {
		t.Errorf("错误信息应说明地址池问题，实际 %q", err)
	}
}

// 套餐限制内网 IPv4 为 1 时，第二张内网网卡不再分配 IPv4，
// 否则「每张网卡自动配 IP」会把套餐里的地址额度撑破。
func TestNetCheckRespectsV4Limit(t *testing.T) {
	svc := newTestIPAllocService(t, testNatPool)
	limit := 1
	vm := &model.VMConfig{
		VmUUID: "vm-1",
		Ip4Pri: &limit,
		NicAll: map[string]model.NCConfig{
			"ethernet0": {NicName: "ethernet0", NicType: "nat"},
			"ethernet1": {NicName: "ethernet1", NicType: "nat"},
		},
	}
	if err := svc.NetCheck("h1", vm); err != nil {
		t.Fatalf("分配失败: %v", err)
	}
	got := 0
	for _, nic := range vm.NicAll {
		if strings.TrimSpace(nic.IP4Addr) != "" {
			got++
		}
	}
	if got != 1 {
		t.Errorf("内网 IPv4 上限为 1，实际分配了 %d 个", got)
	}
}

// 公网 IPv6 上限为 0 时，公网网卡不应自动分配 IPv6（即使主机配置了 v6 池），
// IPv4 仍按额度正常分配。
func TestNetCheckRespectsV6Limit(t *testing.T) {
	const pools = `{"pub":{"from":"1.1.1.100","nums":5,"type":"pub","vers":"ipv4",` +
		`"gate":"1.1.1.1","mask":"255.255.255.0"},` +
		`"pub6":{"from":"2408:8207:1234:5600::100","nums":10,"type":"pub","vers":"ipv6","prefix":64}}`
	svc := newTestIPAllocService(t, pools)
	zero := 0
	vm := &model.VMConfig{
		VmUUID: "vm-1",
		Ip6Pub: &zero,
		NicAll: map[string]model.NCConfig{
			"ethernet0": {NicName: "ethernet0", NicType: "pub"},
		},
	}
	if err := svc.NetCheck("h1", vm); err != nil {
		t.Fatalf("分配失败: %v", err)
	}
	nic := vm.NicAll["ethernet0"]
	if nic.IP4Addr == "" {
		t.Error("公网 IPv4 在额度内，应正常分配")
	}
	if nic.IP6Addr != "" {
		t.Errorf("公网 IPv6 上限为 0，不应分配，实际 %q", nic.IP6Addr)
	}
}

// /128 只有一个地址：数量配大了也只在第一个地址内分配，第二次必须失败，
// 否则会把不属于这个前缀的地址发给实例。
func TestAllocateIPv6SingleAddress(t *testing.T) {
	pools := []ipPool{{
		Name: "pub6", Vers: "ipv6", Type: "pub",
		From: "240e:3b0:1234:5600::2", Nums: 5,
		Gate: "240e:3b0:1234:5600::1", Prefix: 128,
	}}
	allocated := map[string]bool{}

	ip, gate, mask, ok := allocateIP(pools, "ipv6", "pub", allocated)
	if !ok {
		t.Fatal("/128 池应能分配出唯一地址")
	}
	if ip != "240e:3b0:1234:5600::2" {
		t.Errorf("应分配池首地址，实际 %q", ip)
	}
	if gate != "240e:3b0:1234:5600::1" {
		t.Errorf("网关应为池里配置的地址，实际 %q", gate)
	}
	if mask != "128" {
		t.Errorf("掩码应为前缀长度 128，实际 %q", mask)
	}

	allocated[ip] = true
	if _, _, _, ok := allocateIP(pools, "ipv6", "pub", allocated); ok {
		t.Error("/128 只有一个地址，不应继续分配下一个地址")
	}
}

// /112 小段（段内只有 65536 个地址）：递增到段尾后必须停下，不能把段外地址分出去。
func TestAllocateIPv6StopsAtPrefixBoundary(t *testing.T) {
	pools := []ipPool{{
		Name: "pub6", Vers: "ipv6", Type: "pub",
		From: "2408:8207:1234:5600::fffe", Nums: 5, Prefix: 112,
	}}
	allocated := map[string]bool{}

	if ip, _, _, ok := allocateIP(pools, "ipv6", "pub", allocated); !ok || ip != "2408:8207:1234:5600::fffe" {
		t.Fatalf("第一个地址应为 ::fffe，实际 %q ok=%v", ip, ok)
	}
	allocated["2408:8207:1234:5600::fffe"] = true

	if ip, _, _, ok := allocateIP(pools, "ipv6", "pub", allocated); !ok || ip != "2408:8207:1234:5600::ffff" {
		t.Fatalf("第二个地址应为 ::ffff，实际 %q ok=%v", ip, ok)
	}
	allocated["2408:8207:1234:5600::ffff"] = true

	if ip, _, _, ok := allocateIP(pools, "ipv6", "pub", allocated); ok {
		t.Errorf("已越出 /112 网段，不应继续分配，实际又给出 %q", ip)
	}
}

// 分配 IPv6 时要把地址池里的网关写进网卡，静态配置才有默认路由。
func TestNetCheckFillsIPv6Gate(t *testing.T) {
	const pools = `{"pub6":{"from":"240e:3b0:1234:5600::2","nums":2,"type":"pub","vers":"ipv6",` +
		`"gate":"240e:3b0:1234:5600::1","prefix":128}}`
	svc := newTestIPAllocService(t, pools)
	vm := &model.VMConfig{
		VmUUID: "vm-1",
		NicAll: map[string]model.NCConfig{
			"ethernet0": {NicName: "ethernet0", NicType: "pub"},
		},
	}
	if err := svc.NetCheck("h1", vm); err != nil {
		t.Fatalf("分配失败: %v", err)
	}
	nic := vm.NicAll["ethernet0"]
	if nic.IP6Addr != "240e:3b0:1234:5600::2" {
		t.Errorf("IPv6 地址应为池首地址，实际 %q", nic.IP6Addr)
	}
	if nic.IP6Gate != "240e:3b0:1234:5600::1" {
		t.Errorf("IPv6 网关未回填，实际 %q", nic.IP6Gate)
	}
	if nic.IPv6PrefixLen() != 128 {
		t.Errorf("前缀长度应为 128，实际 %d", nic.IPv6PrefixLen())
	}
}

// IPv6 端到端：主控从地址池分配 → 回填网关与前缀 → 下发爱快 DHCPv6 静态绑定。
func TestIPv6EndToEndAllocateAndBind(t *testing.T) {
	f := &fakeRouter{ver: ikuaiVer3, sessKey: "k3"}
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)

	db, err := data.NewDB(t.TempDir())
	if err != nil {
		t.Fatalf("创建临时数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.CreateHost(&data.HSConfigRow{
		HsName:     "h1",
		ServerType: "PromoxSetup",
		IKuaiAddr:  srv.URL,
		IKuaiUser:  "admin",
		IKuaiPass:  "pwd",
		IPAddrMaps: `{"pub6":{"from":"240e:3b0:1234:5600::2","nums":2,"type":"pub",` +
			`"vers":"ipv6","gate":"240e:3b0:1234:5600::1","prefix":64}}`,
	}); err != nil {
		t.Fatalf("写入主机配置失败: %v", err)
	}

	vm := &model.VMConfig{VmUUID: "vm-v6", NicAll: map[string]model.NCConfig{
		"ethernet0": {NicName: "ethernet0", NicType: "pub"},
	}}
	if err := NewIPAllocService(db).NetCheck("h1", vm); err != nil {
		t.Fatalf("分配 IPv6 失败: %v", err)
	}

	nic := vm.NicAll["ethernet0"]
	if nic.IP6Addr != "240e:3b0:1234:5600::2" {
		t.Fatalf("IPv6 地址应为池首地址，实际 %q", nic.IP6Addr)
	}
	if nic.IP6Gate != "240e:3b0:1234:5600::1" {
		t.Errorf("IPv6 网关未回填，实际 %q", nic.IP6Gate)
	}
	if nic.IPv6PrefixLen() != 64 {
		t.Errorf("前缀长度应为 64，实际 %d", nic.IPv6PrefixLen())
	}
	if nic.MacAddr == "" {
		t.Fatal("MAC 应由 IPv6 地址推导出来，否则爱快绑定无从下发")
	}

	res, err := NewNATService(db).BindStaticForVM("h1", vm.VmUUID, vm)
	if err != nil {
		t.Fatalf("下发静态绑定失败: %v", err)
	}
	if res.BoundV6 != 1 {
		t.Fatalf("应下发 1 条 IPv6 静态绑定，实际 %+v", res)
	}

	p := f.lastCallParam(t)
	if strVal(p["mac"]) != nic.MacAddr {
		t.Errorf("下发的 MAC 与网卡不一致：%v != %q", p["mac"], nic.MacAddr)
	}
	if strVal(p["suffix"]) != nic.IPv6Suffix() {
		t.Errorf("下发的后缀应为 %q，实际 %q", nic.IPv6Suffix(), strVal(p["suffix"]))
	}
}
