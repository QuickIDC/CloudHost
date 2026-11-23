package driver

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/luthermonson/go-proxmox"

	"github.com/QuickIDC/HostAgent-Go/internal/model"
)

// SeedISOAttacher 可选能力：把 cidata seed ISO 挂到实例的第二块光驱。
//
// 不用挂载安装 ISO 的那个光驱：实例装系统时光驱里是安装盘，
// seed ISO 必须独占一个设备，开机与重装都不会互相覆盖。
type SeedISOAttacher interface {
	AttachSeedISO(vmUUID, isoName string) (*model.ZMessage, error)
}

// seedISODir 返回主机上 ISO 所在目录（各驱动配置项不同）。
func seedISODir(dvdrom, images string) string {
	if strings.TrimSpace(dvdrom) != "" {
		return dvdrom
	}
	return images
}

// AttachSeedISO 为 PVE 挂一块 ide3 光驱。
func (d *ProxmoxDriver) AttachSeedISO(vmUUID, isoName string) (*model.ZMessage, error) {
	vm, err := d.getVM(vmUUID)
	if err != nil {
		return &model.ZMessage{Success: false, Action: "SeedISO", Message: err.Error()}, nil
	}
	storage := strings.TrimSpace(d.Config.DvdromPath)
	if storage == "" {
		storage = strings.TrimSpace(d.Config.ImagesPath)
	}
	if idx := strings.Index(storage, ":"); idx >= 0 {
		storage = strings.TrimSpace(storage[:idx])
	}
	if storage == "" || strings.Contains(storage, "/") {
		storage = "local"
	}
	value := fmt.Sprintf("%s:iso/%s,media=cdrom", storage, isoName)
	if _, err := vm.Config(d.Ctx(vmUUID), proxmox.VirtualMachineOption{Name: "ide3", Value: value}); err != nil {
		return &model.ZMessage{Success: false, Action: "SeedISO",
			Message: fmt.Sprintf("挂载 cidata ISO 失败: %v（%s）", err, value)}, nil
	}
	return &model.ZMessage{Success: true, Action: "SeedISO", Message: "cidata ISO 已挂载（ide3）"}, nil
}

// AttachSeedISO 为 Hyper-V 增加一块 DVD 驱动器。
func (d *HyperVDriver) AttachSeedISO(vmUUID, isoName string) (*model.ZMessage, error) {
	dir := seedISODir(d.Config.DvdromPath, d.Config.ImagesPath)
	if dir == "" {
		return &model.ZMessage{Success: false, Action: "SeedISO", Message: "主机未配置镜像目录"}, nil
	}
	path := filepath.Join(dir, isoName)
	script := fmt.Sprintf("Add-VMDvdDrive -VMName '%s' -Path '%s' -ErrorAction Stop",
		psEscape(vmUUID), psEscape(path))
	if out, err := d.execPS(script); err != nil {
		return &model.ZMessage{Success: false, Action: "SeedISO",
			Message: fmt.Sprintf("挂载 cidata ISO 失败: %v (%s)", err, strings.TrimSpace(out))}, nil
	}
	return &model.ZMessage{Success: true, Action: "SeedISO", Message: "cidata ISO 已挂载"}, nil
}

// AttachSeedISO 为 VirtualBox 挂第二块光驱（SATA 端口 1）。
func (d *VirtualBoxDriver) AttachSeedISO(vmUUID, isoName string) (*model.ZMessage, error) {
	dir := seedISODir(d.Config.DvdromPath, d.Config.ImagesPath)
	if dir == "" {
		return &model.ZMessage{Success: false, Action: "SeedISO", Message: "主机未配置镜像目录"}, nil
	}
	path := filepath.Join(dir, isoName)
	out, err := d.execVBox("storageattach", vmUUID,
		"--storagectl", "SATA", "--port", "1", "--device", "0",
		"--type", "dvddrive", "--medium", path)
	if err != nil {
		return &model.ZMessage{Success: false, Action: "SeedISO",
			Message: fmt.Sprintf("挂载 cidata ISO 失败: %v (%s)", err, strings.TrimSpace(out))}, nil
	}
	return &model.ZMessage{Success: true, Action: "SeedISO", Message: "cidata ISO 已挂载"}, nil
}

// AttachSeedISO 为 ESXi 新增一块 CD-ROM（不动已有安装盘）。
func (d *ESXiDriver) AttachSeedISO(vmUUID, isoName string) (*model.ZMessage, error) {
	vm, err := d.findVM(vmUUID)
	if err != nil {
		return &model.ZMessage{Success: false, Action: "SeedISO", Message: err.Error()}, nil
	}
	devices, err := vm.Device(d.Ctx(""))
	if err != nil {
		return &model.ZMessage{Success: false, Action: "SeedISO", Message: err.Error()}, nil
	}
	controller, cerr := devices.FindDiskController("")
	if cerr != nil {
		return &model.ZMessage{Success: false, Action: "SeedISO", Message: "未找到控制器: " + cerr.Error()}, nil
	}
	cdrom, err := devices.CreateCdrom(controller)
	if err != nil {
		return &model.ZMessage{Success: false, Action: "SeedISO", Message: "创建光驱失败: " + err.Error()}, nil
	}
	dir := seedISODir(d.Config.DvdromPath, d.Config.ImagesPath)
	isoPath := esxiDatastorePath(dir, isoName)
	devices.InsertIso(cdrom, isoPath)
	if err := vm.AddDevice(d.Ctx(""), cdrom); err != nil {
		return &model.ZMessage{Success: false, Action: "SeedISO",
			Message: fmt.Sprintf("挂载 cidata ISO 失败: %v", err)}, nil
	}
	return &model.ZMessage{Success: true, Action: "SeedISO", Message: "cidata ISO 已挂载"}, nil
}
