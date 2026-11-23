package cidata

import (
	"encoding/binary"
	"os"
	"strings"
	"testing"
	"unicode/utf16"
)

// 用最小的 ISO9660 解析器回读我们生成的镜像：卷标、Joliet 转义、目录项、
// 文件内容都要对得上，否则 guest 里会找不到 hostagent-net.json。
func TestBuildISOReadback(t *testing.T) {
	payload := []byte("{\n  \"nics\": []\n}\n")
	img := BuildISO(map[string][]byte{FileNameHostagentNet: payload})

	// PVD
	pvd := img[secPVD*sectorSize:]
	if pvd[0] != 1 || string(pvd[1:6]) != "CD001" || pvd[6] != 1 {
		t.Fatalf("PVD 头不正确: type=%d id=%q ver=%d", pvd[0], pvd[1:6], pvd[6])
	}
	label := strings.TrimRight(string(pvd[40:72]), " \x00")
	if label != VolumeLabel {
		t.Errorf("卷标应为 %q，实际 %q", VolumeLabel, label)
	}
	if got := binary.LittleEndian.Uint32(pvd[80:84]); got != uint32(len(img)/sectorSize) {
		t.Errorf("卷空间大小不正确: %d", got)
	}

	// SVD（Joliet）
	svd := img[secSVD*sectorSize:]
	if svd[0] != 2 || string(svd[1:6]) != "CD001" {
		t.Fatalf("SVD 头不正确")
	}
	if string(svd[88:91]) != "%/E" {
		t.Errorf("Joliet 转义序列应为 %%/E，实际 %q", svd[88:91])
	}

	extent, size := rootDir(img, svd)
	dir := img[extent*sectorSize : extent*sectorSize+size]

	for off := 0; off+33 <= len(dir); {
		length := int(dir[off])
		if length == 0 {
			break
		}
		rec := dir[off : off+length]
		nameLen := int(rec[32])
		if nameLen > 1 {
			name := decodeUTF16(rec[33 : 33+nameLen])
			if name == FileNameHostagentNet {
				fe := int(binary.LittleEndian.Uint32(rec[2:6]))
				fs := int(binary.LittleEndian.Uint32(rec[10:14]))
				got := img[fe*sectorSize : fe*sectorSize+fs]
				if string(got) != string(payload) {
					t.Fatalf("文件内容不一致:\n%q\n%q", got, payload)
				}
				return
			}
		}
		off += length
	}
	t.Fatalf("Joliet 根目录里找不到 %s", FileNameHostagentNet)
}

func rootDir(img []byte, d []byte) (extent, size int) {
	rec := d[156:190]
	extent = int(binary.LittleEndian.Uint32(rec[2:6]))
	size = int(binary.LittleEndian.Uint32(rec[10:14]))
	return extent, size
}

func decodeUTF16(b []byte) string {
	units := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		units = append(units, binary.BigEndian.Uint16(b[i:]))
	}
	return string(utf16.Decode(units))
}

// WriteISO 必须能在没有 genisoimage/mkisofs 的机器上直接产出镜像。
func TestWriteISOFile(t *testing.T) {
	out := t.TempDir() + "/seed.iso"
	files, err := Files(staticVM())
	if err != nil {
		t.Fatalf("生成内容失败: %v", err)
	}
	if err := WriteISO(out, files); err != nil {
		t.Fatalf("打包失败: %v", err)
	}
	st, err := os.Stat(out)
	if err != nil || st.Size() == 0 {
		t.Fatalf("镜像未生成: %v", err)
	}
	if st.Size()%sectorSize != 0 {
		t.Errorf("镜像大小不是扇区整数倍: %d", st.Size())
	}
	t.Logf("生成镜像 %d 字节", st.Size())
}
