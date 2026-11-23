package driver

import (
	"fmt"
	"strings"

	"github.com/QuickIDC/HostAgent-Go/internal/model"
)

// InjectGuestIP 通过 adb 在安卓实例里配置静态地址。
//
// 模拟器没有光驱、也没有串口，seed ISO 那套用不上；
// 主控直接在宿主侧用 memuc adb 下发 ip 命令，再回读确认。
func (d *MemuDriver) InjectGuestIP(vmUUID string, nic model.NCConfig) (*model.ZMessage, error) {
	idx := d.vmIndex(vmUUID)
	if idx == "" {
		return &model.ZMessage{Success: false, Action: "GuestIP", Message: "无法从实例标识解析模拟器索引"}, nil
	}

	ip6 := strings.TrimSpace(nic.IP6Addr)
	ip4 := strings.TrimSpace(nic.IP4Addr)
	if ip6 == "" && ip4 == "" {
		return &model.ZMessage{Success: false, Action: "GuestIP", Message: "网卡没有可配置的地址"}, nil
	}

	// 安卓侧接口名不固定，按常见顺序探测
	iface := d.guestIface(idx, ip6, ip4)
	if iface == "" {
		return &model.ZMessage{Success: false, Action: "GuestIP", Message: "实例内没有可用的网络接口"}, nil
	}

	var cmds [][]string
	if ip6 != "" {
		cmds = append(cmds, []string{"shell", "su", "-c",
			fmt.Sprintf("ip -6 addr replace %s/%d dev %s", ip6, nic.IPv6PrefixLen(), iface)})
		if gw := strings.TrimSpace(nic.IP6Gate); gw != "" {
			cmds = append(cmds, []string{"shell", "su", "-c",
				fmt.Sprintf("ip -6 route replace default via %s dev %s", gw, iface)})
		}
	}
	if ip4 != "" {
		cmds = append(cmds, []string{"shell", "su", "-c",
			fmt.Sprintf("ip addr replace %s/%d dev %s", ip4, maskToPrefixInt(nic.NicMask), iface)})
		if gw := strings.TrimSpace(nic.NicGate); gw != "" {
			cmds = append(cmds, []string{"shell", "su", "-c",
				fmt.Sprintf("ip route replace default via %s dev %s", gw, iface)})
		}
	}

	for _, c := range cmds {
		if out, err := d.runAdb(idx, c...); err != nil {
			return &model.ZMessage{Success: false, Action: "GuestIP",
				Message: fmt.Sprintf("adb 下发失败: %v (%s)", err, strings.TrimSpace(out))}, nil
		}
	}

	// 回读确认
	out, err := d.runAdb(idx, "shell", "ip", "addr", "show", iface)
	if err != nil || (ip6 != "" && !strings.Contains(out, ip6)) {
		return &model.ZMessage{Success: false, Action: "GuestIP",
			Message: "adb 已执行，但地址未在实例内生效"}, nil
	}
	return &model.ZMessage{Success: true, Action: "GuestIP",
		Message: fmt.Sprintf("已在 %s 配置静态地址", iface)}, nil
}

// guestIface 依次尝试常见安卓网卡名，找不到时回退到默认路由所在接口。
func (d *MemuDriver) guestIface(idx, ip6, ip4 string) string {
	for _, name := range []string{"eth0", "wlan0", "radio0", "rmnet_data0"} {
		if out, err := d.runAdb(idx, "shell", "ip", "addr", "show", name); err == nil && strings.Contains(out, name) {
			return name
		}
	}
	if out, err := d.runAdb(idx, "shell", "ip", "route"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			for i, f := range fields {
				if f == "dev" && i+1 < len(fields) {
					return fields[i+1]
				}
			}
		}
	}
	return ""
}
