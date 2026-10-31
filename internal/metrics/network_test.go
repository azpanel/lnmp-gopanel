package metrics

import (
	"net"
	"reflect"
	"testing"
)

func TestPrimaryInterfacePrefersDefaultRouteAndPhysicalFallback(t *testing.T) {
	up := net.FlagUp | net.FlagRunning
	interfaces := []net.Interface{
		{Index: 1, Name: "lo", Flags: up | net.FlagLoopback},
		{Index: 2, Name: "docker0", Flags: up},
		{Index: 4, Name: "wlan0", Flags: up},
		{Index: 3, Name: "eth0", Flags: up},
		{Index: 5, Name: "eth1"},
	}
	for _, tc := range []struct {
		defaults []string
		want     string
	}{
		{[]string{"wlan0"}, "wlan0"},
		{[]string{"missing", "eth0"}, "eth0"},
		{[]string{"eth1"}, "eth1"},
		{[]string{"lo"}, "eth0"},
		{nil, "eth0"},
	} {
		got := primaryInterface(interfaces, tc.defaults)
		if got == nil || got.Name != tc.want {
			t.Fatalf("defaults %v: got %+v, want %s", tc.defaults, got, tc.want)
		}
	}
	if got := primaryInterface(interfaces[:1], nil); got != nil {
		t.Fatal("loopback selected", got)
	}
	if got := primaryInterface(nil, nil); got != nil {
		t.Fatal("missing interface selected", got)
	}
	// An administratively disabled physical NIC should not appear connected just
	// because a local container bridge is up.
	got := primaryInterface([]net.Interface{interfaces[1], interfaces[4]}, nil)
	if got == nil || got.Name != "eth1" {
		t.Fatal(got)
	}
}
func TestInterfaceAddressesKeepHostAddressesAndIPv6Zone(t *testing.T) {
	addresses := []net.Addr{}
	for _, text := range []string{"10.3.0.13/22", "::ffff:10.3.0.13/128", "2001:db8::2/64", "fe80::1234/64", "127.0.0.1/8", "::1/128", "0.0.0.0/0", "::/0", "224.0.0.1/4", "ff02::1/16"} {
		ip, network, err := net.ParseCIDR(text)
		if err != nil {
			t.Fatal(err)
		}
		addresses = append(addresses, &net.IPNet{IP: ip, Mask: network.Mask})
	}
	addresses = append(addresses, &net.IPAddr{IP: net.ParseIP("10.3.0.13")}, &net.IPAddr{})
	v4, v6 := interfaceAddresses(addresses, "eth0")
	if !reflect.DeepEqual(v4, []string{"10.3.0.13"}) || !reflect.DeepEqual(v6, []string{"2001:db8::2", "fe80::1234%eth0"}) {
		t.Fatal(v4, v6)
	}
	v4, v6 = interfaceAddresses(nil, "eth0")
	if v4 == nil || v6 == nil || len(v4) != 0 || len(v6) != 0 {
		t.Fatal("empty addresses must serialize as arrays", v4, v6)
	}
}
