//go:build linux

package metrics

import (
	"bufio"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

func defaultRouteInterfaces() []string {
	names := []string{}
	for _, entry := range []struct {
		path string
		ipv6 bool
	}{{"/proc/net/route", false}, {"/proc/net/ipv6_route", true}} {
		file, err := os.Open(entry.path)
		if err != nil {
			continue
		}
		names = append(names, routeInterfaces(io.LimitReader(file, 256*1024), entry.ipv6)...)
		file.Close()
	}
	return names
}

func routeInterfaces(reader io.Reader, ipv6 bool) []string {
	type route struct {
		name   string
		metric uint64
	}
	routes := []route{}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		var name, flagsText, metricText string
		base := 10
		if ipv6 {
			if len(fields) < 10 || fields[0] != strings.Repeat("0", 32) || fields[1] != "00" {
				continue
			}
			name, flagsText, metricText, base = fields[9], fields[8], fields[5], 16
		} else {
			if len(fields) < 8 || fields[1] != "00000000" || fields[7] != "00000000" {
				continue
			}
			name, flagsText, metricText = fields[0], fields[3], fields[6]
		}
		flags, err := strconv.ParseUint(flagsText, 16, 64)
		if err != nil || flags&1 == 0 || flags&0x200 != 0 {
			continue
		}
		metric, err := strconv.ParseUint(metricText, base, 64)
		if err != nil {
			continue
		}
		routes = append(routes, route{name, metric})
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].metric != routes[j].metric {
			return routes[i].metric < routes[j].metric
		}
		return routes[i].name < routes[j].name
	})
	names := make([]string, 0, len(routes))
	for _, item := range routes {
		names = append(names, item.name)
	}
	return names
}
