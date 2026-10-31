package metrics

import (
	"errors"
	"net"
	"sort"
	"strings"
)

// NetworkInfo reports the selected interface; Network remains the host total.
type NetworkInfo struct {
	Name  string   `json:"name"`
	Up    bool     `json:"up"`
	IPv4  []string `json:"ipv4"`
	IPv6  []string `json:"ipv6"`
	Rates *Rates   `json:"rates"`
}

func currentNetworkInfo(interfaceRates map[string]*Rates) (*NetworkInfo, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, errors.New("网络接口读取失败")
	}
	primary := primaryInterface(interfaces, defaultRouteInterfaces())
	if primary == nil {
		return nil, errors.New("未发现非回环网卡")
	}
	addresses, err := primary.Addrs()
	if err != nil {
		return nil, errors.New("网卡地址读取失败")
	}
	ipv4, ipv6 := interfaceAddresses(addresses, primary.Name)
	return &NetworkInfo{
		Name: primary.Name, Up: primary.Flags&net.FlagUp != 0 && primary.Flags&net.FlagRunning != 0,
		IPv4: ipv4, IPv6: ipv6, Rates: interfaceRates[primary.Name],
	}, nil
}

func primaryInterface(interfaces []net.Interface, defaults []string) *net.Interface {
	for _, name := range defaults {
		for i := range interfaces {
			if interfaces[i].Name == name && interfaces[i].Flags&net.FlagLoopback == 0 {
				return &interfaces[i]
			}
		}
	}
	// Without a default route, prefer a physical-style NIC over local container
	// bridges, then prefer enabled/running links. Keep selection deterministic.
	var selected *net.Interface
	best := -1
	for i := range interfaces {
		item := &interfaces[i]
		if item.Flags&net.FlagLoopback != 0 {
			continue
		}
		score := 16
		for _, prefix := range []string{"docker", "veth", "br-", "virbr", "vnet", "cni", "flannel"} {
			if strings.HasPrefix(item.Name, prefix) {
				score = 0
				break
			}
		}
		if item.Flags&net.FlagUp != 0 {
			score += 4
		}
		if item.Flags&net.FlagRunning != 0 {
			score += 2
		}
		if score > best || score == best && selected != nil && item.Index < selected.Index {
			selected, best = item, score
		}
	}
	return selected
}

func interfaceAddresses(addresses []net.Addr, name string) ([]string, []string) {
	ipv4, ipv6 := []string{}, []string{}
	seen := map[string]bool{}
	for _, address := range addresses {
		var ip net.IP
		switch value := address.(type) {
		case *net.IPNet:
			ip = value.IP
		case *net.IPAddr:
			ip = value.IP
		}
		if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() {
			continue
		}
		text := ip.String()
		if seen[text] {
			continue
		}
		seen[text] = true
		if ip.To4() != nil {
			ipv4 = append(ipv4, text)
		} else {
			if ip.IsLinkLocalUnicast() {
				text += "%" + name
			}
			ipv6 = append(ipv6, text)
		}
	}
	sort.Strings(ipv4)
	sort.Strings(ipv6)
	return ipv4, ipv6
}
