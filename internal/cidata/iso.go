package cidata

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// WriteISO 把 cidata 内容打包成 ISO。
//
// 优先用 cloud-localds（和 QEMU 驱动同一套工具），再退回 genisoimage /
// mkisofs / xorriso；都不可用时返回明确错误，由调用方记录并继续创建
// （不能因为打包失败让虚拟机创建不出来）。
func WriteISO(outPath string, files map[string][]byte) error {
	if len(files) == 0 {
		return fmt.Errorf("cidata 内容为空")
	}
	dir, err := os.MkdirTemp("", "hostagent-cidata-")
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(dir)

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			return fmt.Errorf("写入 %s 失败: %w", name, err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("创建输出目录失败: %w", err)
	}
	_ = os.Remove(outPath)

	type attempt struct {
		bin  string
		args []string
	}
	attempts := []attempt{
		{"cloud-localds", []string{outPath, filepath.Join(dir, "user-data"), filepath.Join(dir, "meta-data"),
			"--network-config=" + filepath.Join(dir, "network-config")}},
		{"genisoimage", []string{"-quiet", "-volid", "cidata", "-joliet", "-rock", "-output", outPath, dir}},
		{"mkisofs", []string{"-quiet", "-volid", "cidata", "-joliet", "-rock", "-output", outPath, dir}},
		{"xorriso", []string{"-as", "mkisofs", "-volid", "cidata", "-joliet", "-rock", "-o", outPath, dir}},
	}

	var lastErr error
	for _, a := range attempts {
		bin, err := exec.LookPath(a.bin)
		if err != nil {
			continue
		}
		cmd := exec.Command(bin, a.args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			lastErr = fmt.Errorf("%s: %v (%s)", a.bin, err, string(out))
			continue
		}
		if st, err := os.Stat(outPath); err == nil && st.Size() > 0 {
			return nil
		}
		lastErr = fmt.Errorf("%s 未生成文件", a.bin)
	}
	if lastErr == nil {
		return fmt.Errorf("未找到可用的 ISO 打包工具（需要 cloud-localds、genisoimage、mkisofs 或 xorriso 之一）")
	}
	return fmt.Errorf("打包 cidata ISO 失败: %w", lastErr)
}
