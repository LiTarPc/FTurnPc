package backend

import (
	"bufio"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const wgIface = "wg-turn"

// vkExcludeCIDRs — подсети которые должны идти напрямую, а не через туннель.
var vkExcludeCIDRs = []string{
	"87.240.128.0/18",  // VK
	"87.240.192.0/19",  // VK
	"90.156.0.0/16",    // VK TURN (90.156.234.x, 90.156.236.x и др.)
	"93.186.224.0/21",  // VK
	"95.142.192.0/21",  // VK
	"95.163.0.0/16",    // VK TURN (95.163.34.x и др.)
	"95.213.0.0/18",    // VK (id.vk.ru, login.vk.com)
	"155.212.192.0/20", // OK/VK (calls.okcdn.ru)
	"185.16.28.0/22",   // VK
	"194.67.64.0/18",   // VK
	"195.82.146.0/23",  // VK
	"213.180.193.0/24", // Яндекс DNS
	"77.88.0.0/18",     // Яндекс
	"8.8.8.0/24",       // Google DNS
	"1.1.1.0/24",       // Cloudflare DNS
}

// wg-quick-only fields that wg setconf doesn't understand
var wgQuickOnlyFields = map[string]bool{
	"address": true, "dns": true, "mtu": true,
	"preup": true, "postup": true, "predown": true, "postdown": true,
	"saveconfig": true,
}

// parseWGConfig извлекает параметры Address, MTU, AllowedIPs, DNS-серверы и возвращает конфиг, совместимый с wg setconf.
func parseWGConfig(conf string) (addr, mtu string, allowedIPs, dnsServers []string, wgConf string) {
	var out strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(conf))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) == 2 {
			key := strings.ToLower(strings.TrimSpace(parts[0]))
			val := strings.TrimSpace(parts[1])
			switch key {
			case "address":
				addr = val
				continue
			case "mtu":
				mtu = val
				continue
			case "dns":
				for _, d := range strings.Split(val, ",") {
					if item := strings.TrimSpace(d); item != "" {
						dnsServers = append(dnsServers, item)
					}
				}
				continue
			case "allowedips":
				for _, cidr := range strings.Split(val, ",") {
					if c := strings.TrimSpace(cidr); c != "" {
						allowedIPs = append(allowedIPs, c)
					}
				}
			default:
				if wgQuickOnlyFields[key] {
					continue
				}
			}
		}
		out.WriteString(line + "\n")
	}
	if len(dnsServers) == 0 {
		dnsServers = []string{"1.1.1.1", "1.0.0.1"}
	}
	wgConf = out.String()
	return
}

// mergeCIDRs aggregates contiguous or overlapping IPv4 networks to minimize route count.
func mergeCIDRs(cidrs []string) []string {
	var nets []*net.IPNet
	for _, cidr := range cidrs {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err == nil {
			nets = append(nets, ipnet)
		}
	}

	sort.Slice(nets, func(i, j int) bool {
		ipI := nets[i].IP.To4()
		ipJ := nets[j].IP.To4()
		if len(ipI) < 4 || len(ipJ) < 4 {
			return false
		}
		for k := 0; k < 4; k++ {
			if ipI[k] != ipJ[k] {
				return ipI[k] < ipJ[k]
			}
		}
		onesI, _ := nets[i].Mask.Size()
		onesJ, _ := nets[j].Mask.Size()
		return onesI > onesJ
	})

	var merged []*net.IPNet
	for _, n := range nets {
		if len(merged) == 0 {
			merged = append(merged, n)
			continue
		}

		last := merged[len(merged)-1]

		if last.Contains(n.IP) {
			continue
		}

		onesL, _ := last.Mask.Size()
		onesN, _ := n.Mask.Size()
		if onesL == onesN && onesL > 0 {
			superMask := net.CIDRMask(onesL-1, 32)
			superNet := &net.IPNet{IP: last.IP.Mask(superMask), Mask: superMask}
			if superNet.Contains(last.IP) && superNet.Contains(n.IP) {
				merged[len(merged)-1] = superNet
				continue
			}
		}

		merged = append(merged, n)
	}

	var result []string
	for _, n := range merged {
		result = append(result, n.String())
	}
	return result
}

// loadGeoIPRuCIDRs reads geoip-ru.txt, resolves domains to IP addresses, and merges overlapping CIDRs.
func loadGeoIPRuCIDRs() []string {
	var bytes []byte
	exe, err := os.Executable()
	if err == nil {
		txtPath := filepath.Join(filepath.Dir(exe), "geoip-ru.txt")
		bytes, err = os.ReadFile(txtPath)
	}
	if len(bytes) == 0 {
		var err2 error
		bytes, err2 = os.ReadFile("geoip-ru.txt")
		if err2 != nil {
			log.Printf("[WG] Failed to load geoip-ru.txt: %v", err2)
			return nil
		}
	}

	lines := strings.Split(string(bytes), "\n")
	var ruCIDRs []string
	var domainsToResolve []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if _, _, err := net.ParseCIDR(line); err == nil {
			ruCIDRs = append(ruCIDRs, line)
		} else if ip := net.ParseIP(line); ip != nil {
			ruCIDRs = append(ruCIDRs, line+"/32")
		} else {
			domainsToResolve = append(domainsToResolve, line)
		}
	}

	if len(domainsToResolve) > 0 {
		log.Printf("[WG] Resolving %d domains from geoip-ru.txt...", len(domainsToResolve))
		var mu sync.Mutex
		var dnsWg sync.WaitGroup
		sem := make(chan struct{}, 20)
		for _, rawDom := range domainsToResolve {
			dnsWg.Add(1)
			go func(item string) {
				defer dnsWg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				dom := item
				dom = strings.TrimPrefix(dom, "https://")
				dom = strings.TrimPrefix(dom, "http://")
				if idx := strings.Index(dom, "/"); idx != -1 {
					dom = dom[:idx]
				}
				if idx := strings.Index(dom, ":"); idx != -1 {
					dom = dom[:idx]
				}
				dom = strings.TrimSpace(dom)
				if dom == "" {
					return
				}

				ips, err := net.LookupIP(dom)
				if err == nil {
					mu.Lock()
					for _, ip := range ips {
						if ip4 := ip.To4(); ip4 != nil {
							ruCIDRs = append(ruCIDRs, ip4.String()+"/32")
						}
					}
					mu.Unlock()
				} else {
					log.Printf("[WG] Failed to resolve domain %s: %v", dom, err)
				}
			}(rawDom)
		}
		dnsWg.Wait()
	}

	log.Printf("[WG] Loaded %d raw RU routes", len(ruCIDRs))
	ruCIDRs = mergeCIDRs(ruCIDRs)
	log.Printf("[WG] Merged into %d RU routes", len(ruCIDRs))
	return ruCIDRs
}
