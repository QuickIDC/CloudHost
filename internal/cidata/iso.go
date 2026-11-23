package cidata

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteISO 把文件写成 ISO 镜像（纯 Go 实现，不依赖主机上的打包工具）。
func WriteISO(outPath string, files map[string][]byte) error {
	if len(files) == 0 {
		return fmt.Errorf("cidata 内容为空")
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("创建输出目录失败: %w", err)
	}
	img := BuildISO(files)
	if err := os.WriteFile(outPath, img, 0o644); err != nil {
		return fmt.Errorf("写入 ISO 失败: %w", err)
	}
	return nil
}
