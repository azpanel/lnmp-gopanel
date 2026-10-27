// Package ipgeo resolves IPv4 locations using the bundled, read-only QQWry database.
package ipgeo

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"errors"
	"log/slog"
	"net/netip"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
)

//go:embed data/qqwry.dat
var embedded []byte

var bundled, loadErr = newDatabase(embedded)

func init() {
	if loadErr != nil {
		slog.Warn("纯真 IP 数据库加载失败，日志属地显示为未知", "error", loadErr)
	}
}

type database struct {
	data         []byte
	first, count int
}

var errData = errors.New("无效的纯真 IP 数据库")

func pointer(data []byte) int {
	return int(data[0]) | int(data[1])<<8 | int(data[2])<<16
}

func newDatabase(data []byte) (*database, error) {
	if len(data) < 15 {
		return nil, errData
	}
	first, last := int(binary.LittleEndian.Uint32(data)), int(binary.LittleEndian.Uint32(data[4:]))
	if first < 8 || last < first || last > len(data)-7 || (last-first)%7 != 0 {
		return nil, errData
	}
	db := &database{data: data, first: first, count: (last-first)/7 + 1}
	var previous uint32
	for i := 0; i < db.count; i++ {
		index := data[first+i*7 : first+i*7+7]
		start, record := binary.LittleEndian.Uint32(index), pointer(index[4:])
		if (i > 0 && start <= previous) || record < 8 || record > first-5 {
			return nil, errData
		}
		previous = start
	}
	return db, nil
}

// Lookup returns display text, including explicit unsupported/unknown results.
// The database is immutable; concurrent callers share its embedded bytes.
func Lookup(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil || addr.Zone() != "" {
		return "未知"
	}
	addr = addr.Unmap()
	if !addr.Is4() {
		return "暂不支持"
	}
	if bundled == nil {
		return "未知"
	}
	location, err := bundled.lookup(addr)
	if err != nil || location == "" {
		return "未知"
	}
	return location
}

func (db *database) lookup(addr netip.Addr) (string, error) {
	ip := addr.As4()
	value := binary.BigEndian.Uint32(ip[:])
	lo, hi := 0, db.count
	for lo < hi {
		mid := lo + (hi-lo)/2
		start := binary.LittleEndian.Uint32(db.data[db.first+mid*7:])
		if start <= value {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo == 0 {
		return "", nil
	}
	index := db.first + (lo-1)*7
	record := pointer(db.data[index+4 : index+7])
	if value > binary.LittleEndian.Uint32(db.data[record:]) {
		return "", nil
	}
	country, area, err := db.location(record+4, 0)
	if err != nil {
		return "", err
	}
	parts := []string{}
	for _, part := range []string{country, area} {
		decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes([]byte(part))
		if err != nil {
			return "", err
		}
		text := strings.TrimSpace(string(decoded))
		if text != "" && !strings.EqualFold(text, "CZ88.NET") && (len(parts) == 0 || parts[0] != text) {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " "), nil
}

func (db *database) location(offset, depth int) (string, string, error) {
	if offset < 8 || offset >= db.first || depth >= 16 {
		return "", "", errData
	}
	if db.data[offset] == 1 {
		if offset > db.first-4 {
			return "", "", errData
		}
		return db.location(pointer(db.data[offset+1:offset+4]), depth+1)
	}
	country, next, err := db.text(offset, depth)
	if err != nil {
		return "", "", err
	}
	area, _, err := db.text(next, depth)
	return country, area, err
}

// text follows bounded redirects, but next remains after the original field.
func (db *database) text(offset, depth int) (string, int, error) {
	if offset < 8 || offset >= db.first || depth >= 16 {
		return "", 0, errData
	}
	if db.data[offset] == 1 || db.data[offset] == 2 {
		if offset > db.first-4 {
			return "", 0, errData
		}
		target := pointer(db.data[offset+1 : offset+4])
		if target == 0 {
			return "", offset + 4, nil
		}
		value, _, err := db.text(target, depth+1)
		return value, offset + 4, err
	}
	end := offset + min(4096, db.first-offset)
	length := bytes.IndexByte(db.data[offset:end], 0)
	if length < 0 {
		return "", 0, errData
	}
	return string(db.data[offset : offset+length]), offset + length + 1, nil
}
