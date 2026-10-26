//go:build linux

package metrics

import (
	"os"
	"strings"
)

// /sys/block contains whole block devices, avoiding partition double counting.
// Device-mapper nodes are excluded because their physical backing disks are counted.
func wholeDevice(name string) bool {
	if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "dm-") {
		return false
	}
	_, err := os.Stat("/sys/block/" + name)
	return err == nil
}
