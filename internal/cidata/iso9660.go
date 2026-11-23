package cidata

import (
	"encoding/binary"
	"sort"
	"time"
	"unicode/utf16"
)

// 纯 Go 的 ISO9660 + Joliet 打包器。
//
// 不依赖 genisoimage / mkisofs（Windows 主控上通常没有），
// Joliet 目录树保证文件名保持 hostagent-net.json 原样；
// Linux 默认优先读 Joliet，Windows 资源管理器同样能识别。

const sectorSize = 2048

// 扇区布局
const (
	secPVD      = 16
	secSVD      = 17
	secTerm     = 18
	secRootDir  = 19
	secJoliet   = 20
	secLPath    = 21
	secMPath    = 22
	secJLPath   = 23
	secJMPath   = 24
	secDataFrom = 25
)

// BuildISO 生成镜像字节。
func BuildISO(files map[string][]byte) []byte {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	sectors := make([]int, len(names))
	total := secDataFrom
	for i, name := range names {
		sectors[i] = total
		n := (len(files[name]) + sectorSize - 1) / sectorSize
		if n == 0 {
			n = 1
		}
		total += n
	}

	img := make([]byte, total*sectorSize)
	now := time.Now()

	writeDescriptor(img[secPVD*sectorSize:], descriptor{
		primary: true, total: total, rootSector: secRootDir,
		lPath: secLPath, mPath: secMPath, now: now,
	})
	writeDescriptor(img[secSVD*sectorSize:], descriptor{
		joliet: true, total: total, rootSector: secJoliet,
		lPath: secJLPath, mPath: secJMPath, now: now,
	})

	term := img[secTerm*sectorSize:]
	term[0] = 255
	copy(term[1:], "CD001")
	term[6] = 1

	writePathTable(img[secLPath*sectorSize:], secRootDir, false)
	writePathTable(img[secMPath*sectorSize:], secRootDir, true)
	writePathTable(img[secJLPath*sectorSize:], secJoliet, false)
	writePathTable(img[secJMPath*sectorSize:], secJoliet, true)

	writeRoot(img[secRootDir*sectorSize:], files, names, sectors, secRootDir, false, now)
	writeRoot(img[secJoliet*sectorSize:], files, names, sectors, secJoliet, true, now)

	for i, name := range names {
		copy(img[sectors[i]*sectorSize:], files[name])
	}
	return img
}

type descriptor struct {
	primary    bool
	joliet     bool
	total      int
	rootSector int
	lPath      int
	mPath      int
	now        time.Time
}

func writeDescriptor(buf []byte, d descriptor) {
	for i := range buf {
		buf[i] = 0
	}
	if d.joliet {
		buf[0] = 2
	} else {
		buf[0] = 1
	}
	copy(buf[1:6], "CD001")
	buf[6] = 1

	fillSpaces(buf[8:40])  // system identifier
	fillSpaces(buf[40:72]) // volume identifier
	if d.joliet {
		enc := utf16BE(VolumeLabel)
		for i := 40; i < 72; i++ {
			buf[i] = 0
		}
		copy(buf[40:], enc)
	} else {
		copy(buf[40:], VolumeLabel)
	}

	bothU32(buf[80:], uint32(d.total))
	bothU16(buf[120:], 1)             // volume set size
	bothU16(buf[124:], 1)             // volume sequence number
	bothU16(buf[128:], sectorSize)    // logical block size
	bothU32(buf[132:], 10)            // path table size
	binary.LittleEndian.PutUint32(buf[140:], uint32(d.lPath))
	binary.BigEndian.PutUint32(buf[148:], uint32(d.mPath))

	// 根目录记录（名称为 0x00）
	rec := buf[156:190]
	rec[0] = 34
	bothU32(rec[2:], uint32(d.rootSector))
	bothU32(rec[10:], sectorSize)
	copy(rec[18:25], isoDate(d.now))
	rec[25] = 2 // 目录
	bothU16(rec[28:], 1)
	rec[32] = 1
	rec[33] = 0

	fillSpaces(buf[190:318]) // volume set id
	fillSpaces(buf[318:446])
	fillSpaces(buf[446:574])
	fillSpaces(buf[574:702])
	fillSpaces(buf[702:739])
	fillSpaces(buf[739:776])
	fillSpaces(buf[776:813])

	copy(buf[813:830], pvdDate(d.now))
	copy(buf[830:847], pvdDate(d.now))
	fillDigits(buf[847:864])
	fillDigits(buf[864:881])
	buf[881] = 1
	buf[882] = 0

	if d.joliet {
		// 转义序列：%/E 表示 UCS-2 level 3
		copy(buf[88:91], []byte("%/E"))
	}
}

func writePathTable(buf []byte, rootSector int, bigEndian bool) {
	buf[0] = 1
	buf[1] = 0
	if bigEndian {
		binary.BigEndian.PutUint32(buf[2:], uint32(rootSector))
		binary.BigEndian.PutUint16(buf[6:], 1)
	} else {
		binary.LittleEndian.PutUint32(buf[2:], uint32(rootSector))
		binary.LittleEndian.PutUint16(buf[6:], 1)
	}
	buf[8] = 0
}

func writeRoot(buf []byte, files map[string][]byte, names []string, sectors []int, dirSector int, joliet bool, now time.Time) {
	offset := 0
	// "." 与 ".."
	for _, special := range []byte{0, 1} {
		rec := buf[offset:]
		rec[0] = 34
		bothU32(rec[2:], uint32(dirSector))
		bothU32(rec[10:], sectorSize)
		copy(rec[18:25], isoDate(now))
		rec[25] = 2
		bothU16(rec[28:], 1)
		rec[32] = 1
		rec[33] = special
		offset += 34
	}

	for i, name := range names {
		var enc []byte
		if joliet {
			enc = utf16BE(name)
		} else {
			enc = []byte(isoName(name))
		}
		length := 33 + len(enc)
		if len(enc)%2 == 0 {
			length++
		}
		rec := buf[offset : offset+length]
		rec[0] = byte(length)
		bothU32(rec[2:], uint32(sectors[i]))
		bothU32(rec[10:], uint32(len(files[name])))
		copy(rec[18:25], isoDate(now))
		bothU16(rec[28:], 1)
		rec[32] = byte(len(enc))
		copy(rec[33:], enc)
		offset += length
	}
}

// isoName 生成 ISO9660 8.3 文件名（Joliet 不支持时的兜底）。
func isoName(name string) string {
	base, ext := name, ""
	if i := lastDot(name); i > 0 {
		base, ext = name[:i], name[i+1:]
	}
	return sanitize83(base, 8) + "." + sanitize83(ext, 3) + ";1"
}

func lastDot(s string) int {
	for i := len(s) - 1; i > 0; i-- {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}

func sanitize83(s string, max int) string {
	out := make([]byte, 0, max)
	for i := 0; i < len(s) && len(out) < max; i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
			out = append(out, c-32)
		case (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9'):
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "_"
	}
	return string(out)
}

func utf16BE(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := make([]byte, len(units)*2)
	for i, u := range units {
		binary.BigEndian.PutUint16(out[i*2:], u)
	}
	return out
}

func bothU16(buf []byte, v int) {
	binary.LittleEndian.PutUint16(buf[0:], uint16(v))
	binary.BigEndian.PutUint16(buf[2:], uint16(v))
}

func bothU32(buf []byte, v uint32) {
	binary.LittleEndian.PutUint32(buf[0:], v)
	binary.BigEndian.PutUint32(buf[4:], v)
}

func fillSpaces(buf []byte) {
	for i := range buf {
		buf[i] = ' '
	}
}

func fillDigits(buf []byte) {
	for i := range buf {
		buf[i] = '0'
	}
}

func pvdDate(t time.Time) []byte {
	return []byte(t.UTC().Format("20060102150405") + "00" + "0")
}

func isoDate(t time.Time) []byte {
	return []byte{
		byte(t.Year() - 1900), byte(t.Month()), byte(t.Day()),
		byte(t.Hour()), byte(t.Minute()), byte(t.Second()), 0,
	}
}
