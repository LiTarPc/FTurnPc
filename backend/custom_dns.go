package backend

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// buildRemoteDNSServer changes only the DNS used by sing-box for tunneled
// traffic. FreeTurn has its own resolver and never reads this setting.
func buildRemoteDNSServer(raw, fallback, proxyTag string) (map[string]interface{}, error) {
	server := map[string]interface{}{
		"type": "udp", "tag": "dns-remote", "server": fallback, "detour": proxyTag,
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return server, nil
	}
	if net.ParseIP(raw) != nil {
		server["server"] = raw
		return server, nil
	}

	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return nil, fmt.Errorf("DNS сервер: укажите IP, tls://host или https://host/dns-query")
	}
	typ := strings.ToLower(u.Scheme)
	if typ != "udp" && typ != "tls" && typ != "https" {
		return nil, fmt.Errorf("DNS сервер: поддерживаются IP, udp://, tls:// и https://")
	}
	host := u.Hostname()
	if !validDNSHost(host) {
		return nil, fmt.Errorf("DNS сервер: неверное имя или IP %q", host)
	}
	server["type"] = typ
	server["server"] = host
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("DNS сервер: неверный порт %q", port)
		}
		server["server_port"] = n
	}
	if typ == "https" {
		if u.Path != "" && u.Path != "/" {
			server["path"] = u.EscapedPath()
		}
	} else if u.Path != "" && u.Path != "/" {
		return nil, fmt.Errorf("DNS сервер: путь допустим только для https://")
	}
	if net.ParseIP(host) == nil {
		server["domain_resolver"] = "dns-local"
	}
	return server, nil
}

func validDNSHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if c != '-' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
				return false
			}
		}
	}
	return true
}
