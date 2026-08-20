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

var (
	vkExcludeCIDRs []string
	vkExcludeMu    sync.Mutex
	vkExcludeOnce  sync.Once
)

// GetVKExcludeCIDRs возвращает IP-адреса серверов VK, критичных для авторизации и капчи.
func GetVKExcludeCIDRs() []string {
	vkExcludeOnce.Do(func() {
		domains := []string{
			"id.vk.ru",
			"api.vk.ru",
			"login.vk.com",
			"oauth.vk.com",
			"captcha.vk.com",
		}
		var ips []string
		for _, d := range domains {
			addrs, err := net.LookupIP(d)
			if err == nil {
				for _, addr := range addrs {
					if ip4 := addr.To4(); ip4 != nil {
						ips = append(ips, ip4.String()+"/32")
					}
				}
			}
		}
		vkExcludeMu.Lock()
		vkExcludeCIDRs = ips
		vkExcludeMu.Unlock()
	})

	vkExcludeMu.Lock()
	defer vkExcludeMu.Unlock()
	res := make([]string, len(vkExcludeCIDRs))
	copy(res, vkExcludeCIDRs)
	return res
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
