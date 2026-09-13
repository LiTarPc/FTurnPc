package backend

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ConfigType определяет тип входного конфига.
type ConfigType int

const (
	ConfigTypeWG          ConfigType = iota // [Interface]\n...
	ConfigTypeSingboxJSON                   // { "outbounds": [...] }
	ConfigTypeURI                           // vless://... trojan://... ss://...
	ConfigTypeUnknown
)

// DetectConfigType определяет тип конфигурации из поля SB или WGConfig.
func DetectConfigType(sb json.RawMessage, wgConf string) ConfigType {
	sbStr := strings.TrimSpace(string(sb))

	// Если SB задан — определяем его тип
	if len(sbStr) > 0 && sbStr != "null" && sbStr != `""` {
		// Убираем кавычки если SB — это строка
		var unquoted string
		if err := json.Unmarshal(sb, &unquoted); err == nil {
			sbStr = strings.TrimSpace(unquoted)
		}

		if strings.HasPrefix(sbStr, "{") {
			return ConfigTypeSingboxJSON
		}
		for _, scheme := range []string{"vless://", "trojan://", "ss://", "hysteria2://", "tuic://"} {
			if strings.HasPrefix(sbStr, scheme) {
				return ConfigTypeURI
			}
		}
	}

	// Fallback на WGConfig
	if strings.TrimSpace(wgConf) != "" {
		return ConfigTypeWG
	}

	return ConfigTypeUnknown
}

// BuildSingboxConfig — единая точка входа: генерирует полный sing-box JSON из профиля.
func BuildSingboxConfig(profile *ProfileData, params ConnectParams) ([]byte, error) {
	cfgType := DetectConfigType(profile.SB, profile.WGConfig)

	switch cfgType {
	case ConfigTypeWG:
		return buildFromWG(profile.WGConfig, params)
	case ConfigTypeSingboxJSON:
		return buildFromJSON(profile.SB, params)
	case ConfigTypeURI:
		// Извлечём строку URI из JSON RawMessage
		var uri string
		if err := json.Unmarshal(profile.SB, &uri); err != nil {
			uri = strings.TrimSpace(string(profile.SB))
			uri = strings.Trim(uri, `"`)
		}
		return buildFromURI(uri, params)
	default:
		return nil, fmt.Errorf("неизвестный формат конфига: нет ни SB, ни WGConfig")
	}
}

// singboxDir возвращает директорию для конфигов sing-box.
func singboxDir() string {
	dir := filepath.Join(configDir(), "singbox")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// findGeoIPRuSRS ищет файл geoip-ru.srs.
func findGeoIPRuSRS() string {
	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)

	candidates := []string{
		filepath.Join(exeDir, "geoip-ru.srs"),
		filepath.Join(exeDir, "assets", "freeturn", "geoip-ru.srs"),
		filepath.Join(configDir(), "geoip-ru.srs"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// ─── WireGuard → sing-box ──────────────────────────────────────────────────────

func buildFromWG(wgConf string, params ConnectParams) ([]byte, error) {
	addr, mtuStr, _, dnsServers, _ := parseWGConfig(wgConf)
	if addr == "" {
		return nil, fmt.Errorf("Address не найден в WG-конфиге")
	}

	// Извлечение ключей из секций WG-конфига
	var privKey, pubKey, psk string
	var keepalive int
	for _, line := range strings.Split(wgConf, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])
		switch key {
		case "privatekey":
			privKey = val
		case "publickey":
			pubKey = val
		case "presharedkey":
			psk = val
		case "persistentkeepalive":
			keepalive, _ = strconv.Atoi(val)
		}
	}

	if privKey == "" || pubKey == "" {
		return nil, fmt.Errorf("не найдены PrivateKey/PublicKey в WG-конфиге")
	}

	mtu := 1300
	if params.MTU >= 576 && params.MTU <= 1500 {
		mtu = params.MTU
	} else if mtuStr != "" {
		if v, err := strconv.Atoi(mtuStr); err == nil {
			mtu = v
		}
	}

	dnsRemote := "1.1.1.1"
	if len(dnsServers) > 0 {
		dnsRemote = dnsServers[0]
	}

	var localAddresses []string
	for _, a := range strings.Split(addr, ",") {
		a = strings.TrimSpace(a)
		if a != "" && !strings.Contains(a, ":") { // Пропускаем IPv6
			localAddresses = append(localAddresses, a)
		}
	}

	if len(localAddresses) == 0 {
		return nil, fmt.Errorf("Address не содержит валидных IPv4 адресов в WG-конфиге")
	}

	// WireGuard endpoint
	endpoint := map[string]interface{}{
		"type":          "wireguard",
		"tag":           "proxy",
		"local_address": localAddresses,
		"private_key":   privKey,
		"peers": []map[string]interface{}{
			{
				"address":                      "127.0.0.1",
				"port":                         9000,
				"public_key":                   pubKey,
				"allowed_ips":                  []string{"0.0.0.0/0"},
				"persistent_keepalive_interval": max(keepalive, 25),
			},
		},
		"mtu": mtu,
	}
	if psk != "" {
		endpoint["peers"].([]map[string]interface{})[0]["pre_shared_key"] = psk
	}

	return assembleConfig(nil, []interface{}{endpoint}, dnsRemote, mtu+100, params)
}

// ─── sing-box JSON → merge ──────────────────────────────────────────────────────

func buildFromJSON(sbRaw json.RawMessage, params ConnectParams) ([]byte, error) {
	var userCfg map[string]interface{}
	if err := json.Unmarshal(sbRaw, &userCfg); err != nil {
		return nil, fmt.Errorf("невалидный sing-box JSON: %w", err)
	}

	var outbounds []interface{}
	var endpoints []interface{}

	if obs, ok := userCfg["outbounds"].([]interface{}); ok {
		outbounds = obs
	}
	if eps, ok := userCfg["endpoints"].([]interface{}); ok {
		endpoints = eps
	}

	if len(outbounds) == 0 && len(endpoints) == 0 {
		return nil, fmt.Errorf("sing-box JSON не содержит outbounds или endpoints")
	}

	return assembleConfig(outbounds, endpoints, "1.1.1.1", 1400, params)
}

// ─── URI → sing-box outbound ────────────────────────────────────────────────────

func buildFromURI(uri string, params ConnectParams) ([]byte, error) {
	outbound, err := parseProxyURI(uri)
	if err != nil {
		return nil, fmt.Errorf("ошибка парсинга URI: %w", err)
	}
	return assembleConfig([]interface{}{outbound}, nil, "1.1.1.1", 1400, params)
}

// parseProxyURI парсит URI протокола и генерирует sing-box outbound.
func parseProxyURI(uri string) (map[string]interface{}, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("невалидный URI: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)
	host := u.Hostname()
	portStr := u.Port()
	port := 443
	if portStr != "" {
		port, _ = strconv.Atoi(portStr)
	}

	q := u.Query()

	switch scheme {
	case "vless":
		return buildVLESSOutbound(u.User.Username(), host, port, q)
	case "trojan":
		password, _ := u.User.Password()
		if password == "" {
			password = u.User.Username()
		}
		return buildTrojanOutbound(password, host, port, q)
	case "ss":
		return buildShadowsocksOutbound(u.User.String(), host, port)
	case "hysteria2", "hy2":
		return buildHysteria2Outbound(u.User.Username(), host, port, q)
	case "tuic":
		password, _ := u.User.Password()
		return buildTUICOutbound(u.User.Username(), password, host, port, q)
	default:
		return nil, fmt.Errorf("неподдерживаемый протокол: %s", scheme)
	}
}

func buildVLESSOutbound(uuid, host string, port int, q url.Values) (map[string]interface{}, error) {
	out := map[string]interface{}{
		"type":        "vless",
		"tag":         "proxy",
		"server":      "127.0.0.1",
		"server_port": 9000,
		"uuid":        uuid,
	}
	if flow := q.Get("flow"); flow != "" {
		out["flow"] = flow
	}
	addTLS(out, q)
	addTransport(out, q)
	return out, nil
}

func buildTrojanOutbound(password, host string, port int, q url.Values) (map[string]interface{}, error) {
	out := map[string]interface{}{
		"type":        "trojan",
		"tag":         "proxy",
		"server":      "127.0.0.1",
		"server_port": 9000,
		"password":    password,
	}
	addTLS(out, q)
	addTransport(out, q)
	return out, nil
}

func buildShadowsocksOutbound(userinfo, host string, port int) (map[string]interface{}, error) {
	// userinfo может быть base64(method:password) или method:password
	decoded := userinfo
	// Пробуем декодировать base64
	if !strings.Contains(userinfo, ":") {
		// Пробуем base64
		if d, err := url.PathUnescape(userinfo); err == nil {
			decoded = d
		}
	}

	parts := strings.SplitN(decoded, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("невалидный SS userinfo: ожидается method:password")
	}

	return map[string]interface{}{
		"type":        "shadowsocks",
		"tag":         "proxy",
		"server":      "127.0.0.1",
		"server_port": 9000,
		"method":      parts[0],
		"password":    parts[1],
	}, nil
}

func buildHysteria2Outbound(password, host string, port int, q url.Values) (map[string]interface{}, error) {
	out := map[string]interface{}{
		"type":        "hysteria2",
		"tag":         "proxy",
		"server":      "127.0.0.1",
		"server_port": 9000,
		"password":    password,
	}
	if sni := q.Get("sni"); sni != "" {
		out["tls"] = map[string]interface{}{
			"enabled":     true,
			"server_name": sni,
		}
	}
	if obfs := q.Get("obfs"); obfs != "" {
		out["obfs"] = map[string]interface{}{
			"type":     obfs,
			"password": q.Get("obfs-password"),
		}
	}
	return out, nil
}

func buildTUICOutbound(uuid, password, host string, port int, q url.Values) (map[string]interface{}, error) {
	out := map[string]interface{}{
		"type":        "tuic",
		"tag":         "proxy",
		"server":      "127.0.0.1",
		"server_port": 9000,
		"uuid":        uuid,
		"password":    password,
	}
	if cc := q.Get("congestion_control"); cc != "" {
		out["congestion_control"] = cc
	}
	if sni := q.Get("sni"); sni != "" {
		out["tls"] = map[string]interface{}{
			"enabled":     true,
			"server_name": sni,
		}
	}
	return out, nil
}

// ─── Helpers ────────────────────────────────────────────────────────────────────

func addTLS(out map[string]interface{}, q url.Values) {
	security := q.Get("security")
	if security == "" || security == "none" {
		return
	}

	tls := map[string]interface{}{
		"enabled": true,
	}
	if sni := q.Get("sni"); sni != "" {
		tls["server_name"] = sni
	}
	if fp := q.Get("fp"); fp != "" {
		tls["utls"] = map[string]interface{}{
			"enabled":     true,
			"fingerprint": fp,
		}
	}
	if security == "reality" {
		reality := map[string]interface{}{"enabled": true}
		if pbk := q.Get("pbk"); pbk != "" {
			reality["public_key"] = pbk
		}
		if sid := q.Get("sid"); sid != "" {
			reality["short_id"] = sid
		}
		tls["reality"] = reality
	}
	if q.Get("allowInsecure") == "1" {
		tls["insecure"] = true
	}
	out["tls"] = tls
}

func addTransport(out map[string]interface{}, q url.Values) {
	tp := q.Get("type")
	if tp == "" || tp == "tcp" {
		return
	}
	transport := map[string]interface{}{
		"type": tp,
	}
	switch tp {
	case "ws":
		if host := q.Get("host"); host != "" {
			transport["headers"] = map[string]interface{}{
				"Host": host,
			}
		}
		if path := q.Get("path"); path != "" {
			transport["path"] = path
		}
	case "grpc":
		if sn := q.Get("serviceName"); sn != "" {
			transport["service_name"] = sn
		}
	case "h2", "http":
		transport["type"] = "http"
		if host := q.Get("host"); host != "" {
			transport["host"] = []string{host}
		}
		if path := q.Get("path"); path != "" {
			transport["path"] = path
		}
	}
	out["transport"] = transport
}

// assembleConfig собирает полный sing-box конфиг из outbounds/endpoints + стандартный шаблон.
func assembleConfig(outbounds []interface{}, endpoints []interface{}, dnsRemote string, tunMTU int, params ConnectParams) ([]byte, error) {
	// Определяем тег прокси
	proxyTag := findProxyTag(outbounds, endpoints)

	// Формируем outbounds (добавляем direct если нет)
	hasDirect := false
	for _, ob := range outbounds {
		if m, ok := ob.(map[string]interface{}); ok {
			if m["tag"] == "direct" {
				hasDirect = true
				break
			}
		}
	}
	if !hasDirect {
		outbounds = append(outbounds, map[string]interface{}{
			"type": "direct",
			"tag":  "direct",
		})
	}

	// Route rules
	rules := []interface{}{
		map[string]interface{}{"action": "sniff", "timeout": "300ms"},
		map[string]interface{}{"protocol": "dns", "action": "hijack-dns"},
		map[string]interface{}{
			"process_name": []string{"freeturnclient.exe", "freeturnclient"},
			"outbound":     "direct",
		},
		map[string]interface{}{"ip_is_private": true, "outbound": "direct"},
	}

	var ruleSets []interface{}
	if params.BypassRu {
		rules = append(rules, map[string]interface{}{
			"rule_set": []string{"geoip-ru", "ru-domains"},
			"outbound": "direct",
		})
		geoPath := findGeoIPRuSRS()
		if geoPath != "" {
			ruleSets = append(ruleSets, map[string]interface{}{
				"type": "local", "tag": "geoip-ru", "format": "binary", "path": geoPath,
			})
		}
		ruleSets = append(ruleSets, map[string]interface{}{
			"type": "inline",
			"tag":  "ru-domains",
			"rules": []interface{}{
				map[string]interface{}{
					"domain_suffix": []string{".ru", ".su", ".xn--p1ai"},
				},
			},
		})
	}

	// DNS rules
	var dnsRules []interface{}
	if params.BypassRu {
		dnsRules = append(dnsRules, map[string]interface{}{
			"rule_set": "ru-domains",
			"action":   "route",
			"server":   "dns-local",
		})
	}

	// Собираем конфиг
	config := map[string]interface{}{
		"log": map[string]interface{}{
			"level":     "info",
			"timestamp": true,
		},
		"dns": map[string]interface{}{
			"servers": []interface{}{
				map[string]interface{}{
					"type": "udp", "tag": "dns-remote", "server": dnsRemote, "detour": proxyTag,
				},
				map[string]interface{}{
					"type": "local", "tag": "dns-local",
				},
			},
			"rules":    dnsRules,
			"final":    "dns-remote",
			"strategy": "ipv4_only",
		},
		"inbounds": []interface{}{
			map[string]interface{}{
				"type":           "tun",
				"tag":            "tun-in",
				"interface_name": singTunName,
				"address":        []string{"172.19.0.1/30"},
				"mtu":            tunMTU,
				"auto_route":     true,
				"strict_route":   true,
				"stack":          "mixed",
				"dns_mode":       "hijack",
			},
		},
		"outbounds": outbounds,
		"route": map[string]interface{}{
			"rules":                  rules,
			"rule_set":               ruleSets,
			"find_process":           true,
			"final":                  proxyTag,
			"auto_detect_interface":  true,
			"default_domain_resolver": map[string]interface{}{"server": "dns-local"},
		},
		"experimental": map[string]interface{}{
			"cache_file": map[string]interface{}{
				"enabled": true,
				"path":    filepath.Join(singboxDir(), "cache.db"),
			},
		},
	}

	// Endpoints (если есть — для WireGuard)
	if len(endpoints) > 0 {
		config["endpoints"] = endpoints
	}

	return json.MarshalIndent(config, "", "  ")
}

// findProxyTag ищет тег прокси в outbounds/endpoints.
func findProxyTag(outbounds []interface{}, endpoints []interface{}) string {
	// Ищем объект с tag "proxy"
	for _, list := range [][]interface{}{outbounds, endpoints} {
		for _, item := range list {
			if m, ok := item.(map[string]interface{}); ok {
				if tag, ok := m["tag"].(string); ok && tag == "proxy" {
					return tag
				}
			}
		}
	}
	// Fallback: первый не-direct outbound/endpoint
	for _, list := range [][]interface{}{endpoints, outbounds} {
		for _, item := range list {
			if m, ok := item.(map[string]interface{}); ok {
				tag, _ := m["tag"].(string)
				if tag != "direct" && tag != "" {
					return tag
				}
			}
		}
	}
	// Крайний fallback
	return "proxy"
}
