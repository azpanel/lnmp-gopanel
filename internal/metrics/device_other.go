//go:build !linux

package metrics

func wholeDevice(name string) bool { return true }
