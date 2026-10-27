package ipgeo

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func putPointer(data []byte, value int) {
	data[0], data[1], data[2] = byte(value), byte(value>>8), byte(value>>16)
}

// fixture exercises a direct record, mode 1 -> mode 2, and a null area redirect.
func fixture() []byte {
	data := make([]byte, 8)
	add := func(value []byte) int {
		offset := len(data)
		data = append(data, value...)
		return offset
	}
	encoded, _ := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("中国测试省"))
	country := add(append(encoded, 0))
	area := add([]byte("Test ISP\x00"))
	redirect := func(flag byte, target int) []byte {
		out := []byte{flag, 0, 0, 0}
		putPointer(out[1:], target)
		return out
	}
	block := add(append(redirect(2, country), redirect(1, area)...))
	var offsets []int
	for i := range 3 {
		record := make([]byte, 4)
		binary.LittleEndian.PutUint32(record, 0x01000009+uint32(i*20))
		switch i {
		case 0:
			record = append(record, encoded...)
			record = append(record, []byte("\x00CZ88.NET\x00")...)
		case 1:
			record = append(record, redirect(1, block)...)
		case 2:
			record = append(record, redirect(2, country)...)
			record = append(record, redirect(2, 0)...)
		}
		offsets = append(offsets, add(record))
	}
	first := len(data)
	for i, offset := range offsets {
		index := make([]byte, 7)
		binary.LittleEndian.PutUint32(index, 0x01000000+uint32(i*20))
		putPointer(index[4:], offset)
		data = append(data, index...)
	}
	binary.LittleEndian.PutUint32(data, uint32(first))
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-7))
	return data
}

func TestLookupRangesAndRedirects(t *testing.T) {
	db, err := newDatabase(fixture())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ ip, want string }{
		{"0.255.255.255", ""},
		{"1.0.0.0", "中国测试省"},
		{"1.0.0.9", "中国测试省"},
		{"1.0.0.10", ""},
		{"1.0.0.20", "中国测试省 Test ISP"},
		{"1.0.0.29", "中国测试省 Test ISP"},
		{"1.0.0.30", ""},
		{"1.0.0.40", "中国测试省"},
		{"1.0.0.49", "中国测试省"},
		{"255.255.255.255", ""},
	} {
		t.Run(tc.ip, func(t *testing.T) {
			got, err := db.lookup(netip.MustParseAddr(tc.ip))
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestInvalidDatabase(t *testing.T) {
	for _, mutate := range []func([]byte) []byte{
		func(data []byte) []byte { return data[:7] },
		func(data []byte) []byte { return data[:len(data)-1] },
		func(data []byte) []byte { binary.LittleEndian.PutUint32(data, 0); return data },
		func(data []byte) []byte { binary.LittleEndian.PutUint32(data[4:], 0xffffffff); return data },
		func(data []byte) []byte { putPointer(data[len(data)-3:], 0); return data },
		func(data []byte) []byte { copy(data[len(data)-7:], data[len(data)-14:len(data)-7]); return data },
	} {
		if _, err := newDatabase(mutate(fixture())); err == nil {
			t.Fatal("accepted invalid database")
		}
	}
	for _, mutate := range []func(*database){
		func(db *database) { db.data[8] = 2; putPointer(db.data[9:], 8) }, // cyclic text
		func(db *database) { db.data[8] = 2; putPointer(db.data[9:], 0xffffff) },
		func(db *database) {
			index := db.first + 7
			record := pointer(db.data[index+4 : index+7])
			db.data[record+4] = 1
			putPointer(db.data[record+5:], record+4) // cyclic location
		},
		func(db *database) {
			for i := 8; i < db.first; i++ {
				db.data[i] = 'x'
			}
		},
	} {
		db, _ := newDatabase(fixture())
		mutate(db)
		if _, err := db.lookup(netip.MustParseAddr("1.0.0.20")); err == nil {
			t.Fatal("accepted invalid record")
		}
	}
}

func TestBundledDatabase(t *testing.T) {
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(embedded)); got != "15197d3a13d767a3113ca92bde9aa2fa02a99b0620eee07ebbd630262a4f57bf" {
		t.Fatal("bundled database checksum mismatch", got)
	}
	for _, ip := range []string{"114.114.114.114", "8.8.8.8", "1.1.1.1", "127.0.0.1"} {
		if location := Lookup(ip); location == "未知" || strings.Contains(location, "\ufffd") {
			t.Fatalf("unexpected location for %s: %q", ip, location)
		} else {
			t.Log(ip, location)
		}
	}
	for ip, want := range map[string]string{
		"invalid": "未知", "": "未知", "2001:db8::1": "暂不支持", "fe80::1%eth0": "未知",
		"::ffff:114.114.114.114": Lookup("114.114.114.114"),
	} {
		if got := Lookup(ip); got != want {
			t.Errorf("Lookup(%q) = %q; want %q", ip, got, want)
		}
	}
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			for range 100 {
				if got := Lookup("114.114.114.114"); !strings.Contains(got, "中国") {
					t.Errorf("concurrent lookup: %q", got)
				}
			}
		})
	}
	workers.Wait()
}

func FuzzDatabase(f *testing.F) {
	f.Add(fixture())
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		db, err := newDatabase(bytes.Clone(data))
		if err != nil {
			return
		}
		for _, ip := range []string{"0.0.0.0", "1.0.0.0", "1.0.0.20", "1.0.0.40", "255.255.255.255"} {
			_, _ = db.lookup(netip.MustParseAddr(ip))
		}
	})
}
