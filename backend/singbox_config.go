package backend

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
)

// ConfigType determines the source format used for sing-box configuration.
type ConfigType int

const (
	ConfigTypeWG ConfigType = iota
	ConfigTypeSingboxJSON
	ConfigTypeURI
	ConfigTypeUnknown
)

const (
	freeTurnHost = "127.0.0.1"
	freeTurnPort = 9000
)

func DetectConfigType(sb json.RawMessage, wgConf string) ConfigType {
	sbStr := strings.TrimSpace(string(sb))
	if len(sbStr) > 0 && sbStr != "null" && sbStr != `""` {
		if unquoted, err := rawSBString(sb); err == nil {
			sbStr = strings.TrimSpace(unquoted)
		}
		lower := strings.ToLower(sbStr)
		if strings.HasPrefix(sbStr, "{") {
			return ConfigTypeSingboxJSON
		}
		for _, scheme := range []string{"vless://", "trojan://", "ss://", "hysteria2://", "hy2://", "tuic://"} {
			if strings.HasPrefix(lower, scheme) {
				return ConfigTypeURI
			}
		}
	}
	if strings.TrimSpace(wgConf) != "" {
		return ConfigTypeWG
	}
	return ConfigTypeUnknown
}

func BuildSingboxConfig(profile *ProfileData, params ConnectParams) ([]byte, error) {
	if profile == nil {
		return nil, fmt.Errorf("profile is nil")
	}
	switch DetectConfigType(profile.SB, profile.WGConfig) {
	case ConfigTypeWG:
		return buildFromWG(profile.WGConfig, params)
	case ConfigTypeSingboxJSON:
		return buildFromJSON(profile.SB, params)
	case ConfigTypeURI:
		uri, err := rawSBString(profile.SB)
		if err != nil {
			return nil, err
		}
		return buildFromURI(uri, params)
	default:
		return nil, fmt.Errorf("неизвестный формат конфига: нет поддерживаемого SB или WGConfig")
	}
}

func singboxDir() string {
	dir := filepath.Join(configDir(), "singbox")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.Chmod(dir, 0o700)
	return dir
}

func findGeoIPRuSRS() string {
	exe, _ := os.Executable()
	exeDir := filepath.Dir(exe)
	candidates := []string{
		filepath.Join(exeDir, "geoip-ru.srs"),
		filepath.Join(exeDir, "assets", "freeturn", "geoip-ru.srs"),
		filepath.Join(configDir(), "geoip-ru.srs"),
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// ---- Legacy WireGuard -> sing-box endpoint ---------------------------------

type wgLegacyPeer struct {
	PublicKey  string
	PSK        string
	Keepalive  int
	AllowedIPs []string
}

type wgLegacyConfig struct {
	Addresses  []string
	PrivateKey string
	DNS        []string
	MTU        int
	Peers      []wgLegacyPeer
}

func parseLegacyWG(conf string) (wgLegacyConfig, error) {
	var cfg wgLegacyConfig
	section := ""
	peerIndex := -1

	for lineNo, line := range strings.Split(conf, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.ToLower(strings.TrimSpace(trimmed[1 : len(trimmed)-1]))
			if section == "peer" {
				cfg.Peers = append(cfg.Peers, wgLegacyPeer{})
				peerIndex = len(cfg.Peers) - 1
			}
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) != 2 {
			return cfg, fmt.Errorf("WG строка %d: ожидается key=value", lineNo+1)
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.TrimSpace(parts[1])

		switch section {
		case "interface":
			switch key {
			case "address":
				for _, item := range strings.Split(value, ",") {
					item = strings.TrimSpace(item)
					if item != "" {
						cfg.Addresses = append(cfg.Addresses, item)
					}
				}
			case "privatekey":
				cfg.PrivateKey = value
			case "dns":
				for _, item := range strings.Split(value, ",") {
					item = strings.TrimSpace(item)
					if item != "" {
						cfg.DNS = append(cfg.DNS, item)
					}
				}
			case "mtu":
				v, err := strconv.Atoi(value)
				if err != nil {
					return cfg, fmt.Errorf("WG MTU %q не является числом", value)
				}
				cfg.MTU = v
			}
		case "peer":
			if peerIndex < 0 {
				return cfg, fmt.Errorf("WG Peer не инициализирован")
			}
			peer := &cfg.Peers[peerIndex]
			switch key {
			case "publickey":
				peer.PublicKey = value
			case "presharedkey":
				peer.PSK = value
			case "persistentkeepalive":
				v, err := strconv.Atoi(value)
				if err != nil || v < 0 {
					return cfg, fmt.Errorf("WG PersistentKeepalive %q невалиден", value)
				}
				peer.Keepalive = v
			case "allowedips":
				for _, item := range strings.Split(value, ",") {
					item = strings.TrimSpace(item)
					if item != "" {
						peer.AllowedIPs = append(peer.AllowedIPs, item)
					}
				}
			}
		}
	}

	if len(cfg.Addresses) == 0 {
		return cfg, fmt.Errorf("Address не найден в секции [Interface]")
	}
	if cfg.PrivateKey == "" {
		return cfg, fmt.Errorf("PrivateKey не найден в секции [Interface]")
	}
	if len(cfg.Peers) != 1 {
		return cfg, fmt.Errorf("ожидается ровно один [Peer], получено %d", len(cfg.Peers))
	}
	if cfg.Peers[0].PublicKey == "" {
		return cfg, fmt.Errorf("PublicKey не найден в секции [Peer]")
	}
	return cfg, nil
}

func buildFromWG(wgConf string, params ConnectParams) ([]byte, error) {
	wg, err := parseLegacyWG(wgConf)
	if err != nil {
		return nil, err
	}

	var addresses []string
	for _, a := range wg.Addresses {
		ip, _, err := net.ParseCIDR(a)
		if err != nil {
			return nil, fmt.Errorf("невалидный WG Address %q: %w", a, err)
		}
		if ip.To4() != nil {
			addresses = append(addresses, a)
		}
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("WG Address не содержит IPv4 префикса")
	}

	mtu := 1300
	if wg.MTU >= 576 && wg.MTU <= 1500 {
		mtu = wg.MTU
	}
	if params.MTU >= 576 && params.MTU <= 1500 {
		mtu = params.MTU
	}
	tunMTU := mtu + 100
	if tunMTU > 1500 {
		tunMTU = 1500
	}

	dnsRemote := "1.1.1.1"
	if len(wg.DNS) > 0 {
		dnsRemote = wg.DNS[0]
	}

	peer := wg.Peers[0]
	allowedIPs := peer.AllowedIPs
	if len(allowedIPs) == 0 {
		allowedIPs = []string{"0.0.0.0/0"}
	}
	endpointPeer := map[string]interface{}{
		"address":                       freeTurnHost,
		"port":                          freeTurnPort,
		"public_key":                    peer.PublicKey,
		"allowed_ips":                   allowedIPs,
		"persistent_keepalive_interval": max(peer.Keepalive, 25),
	}
	if peer.PSK != "" {
		endpointPeer["pre_shared_key"] = peer.PSK
	}
	endpoint := map[string]interface{}{
		"type":        "wireguard",
		"tag":         "proxy",
		"address":     addresses,
		"private_key": wg.PrivateKey,
		"peers":       []interface{}{endpointPeer},
		"mtu":         mtu,
	}
	return assembleConfig(nil, []interface{}{endpoint}, dnsRemote, tunMTU, params)
}

// ---- sing-box JSON -----------------------------------------------------------

func buildFromJSON(sbRaw json.RawMessage, params ConnectParams) ([]byte, error) {
	var userCfg map[string]interface{}
	if err := json.Unmarshal(sbRaw, &userCfg); err != nil {
		return nil, fmt.Errorf("невалидный sing-box JSON: %w", err)
	}
	outbounds, _ := userCfg["outbounds"].([]interface{})
	endpoints, _ := userCfg["endpoints"].([]interface{})
	if len(outbounds) == 0 && len(endpoints) == 0 {
		return nil, fmt.Errorf("sing-box JSON не содержит outbounds или endpoints")
	}

	// sing-box removed the legacy WireGuard outbound in 1.13. Profiles can still
	// arrive from an older link generator, so migrate them to the endpoint schema
	// before validating the rest of the config.
	var err error
	outbounds, endpoints, err = migrateLegacyWireGuardOutbounds(outbounds, endpoints)
	if err != nil {
		return nil, err
	}
	if err := normalizeJSONProxyTargets(outbounds, endpoints); err != nil {
		return nil, err
	}
	return assembleConfig(outbounds, endpoints, "1.1.1.1", 1400, params)
}

func migrateLegacyWireGuardOutbounds(outbounds, endpoints []interface{}) ([]interface{}, []interface{}, error) {
	kept := make([]interface{}, 0, len(outbounds))
	for _, raw := range outbounds {
		m, ok := raw.(map[string]interface{})
		if !ok {
			return nil, nil, fmt.Errorf("outbound должен быть JSON object")
		}
		typ, _ := m["type"].(string)
		if strings.ToLower(typ) != "wireguard" {
			kept = append(kept, raw)
			continue
		}

		ep, err := legacyWireGuardOutboundToEndpoint(m)
		if err != nil {
			return nil, nil, err
		}
		endpoints = append(endpoints, ep)
	}
	return kept, endpoints, nil
}

func legacyWireGuardOutboundToEndpoint(old map[string]interface{}) (map[string]interface{}, error) {
	address, ok := old["address"]
	if !ok {
		address = old["local_address"]
	}
	if address == nil {
		return nil, fmt.Errorf("legacy WireGuard outbound не содержит local_address/address")
	}
	privateKey, _ := old["private_key"].(string)
	if strings.TrimSpace(privateKey) == "" {
		return nil, fmt.Errorf("legacy WireGuard outbound не содержит private_key")
	}

	ep := map[string]interface{}{
		"type":        "wireguard",
		"address":     address,
		"private_key": privateKey,
	}
	if tag, _ := old["tag"].(string); tag != "" {
		ep["tag"] = tag
	}
	for _, key := range []string{"mtu", "workers"} {
		if v, exists := old[key]; exists {
			ep[key] = v
		}
	}

	var peers []interface{}
	if rawPeers, ok := old["peers"].([]interface{}); ok && len(rawPeers) > 0 {
		if len(rawPeers) != 1 {
			return nil, fmt.Errorf("legacy WireGuard outbound должен содержать ровно один peer для FreeTurn")
		}
		peer, ok := rawPeers[0].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("legacy WireGuard peer должен быть JSON object")
		}
		publicKey, _ := peer["public_key"].(string)
		if strings.TrimSpace(publicKey) == "" {
			return nil, fmt.Errorf("legacy WireGuard peer не содержит public_key")
		}
		newPeer := map[string]interface{}{
			"address":     freeTurnHost,
			"port":        freeTurnPort,
			"public_key":  publicKey,
			"allowed_ips": []interface{}{"0.0.0.0/0"},
		}
		if allowed, exists := peer["allowed_ips"]; exists {
			newPeer["allowed_ips"] = allowed
		}
		for _, key := range []string{"pre_shared_key", "reserved", "persistent_keepalive_interval"} {
			if v, exists := peer[key]; exists {
				newPeer[key] = v
			}
		}
		if _, exists := newPeer["persistent_keepalive_interval"]; !exists {
			newPeer["persistent_keepalive_interval"] = 25
		}
		peers = []interface{}{newPeer}
	} else {
		publicKey, _ := old["peer_public_key"].(string)
		if strings.TrimSpace(publicKey) == "" {
			return nil, fmt.Errorf("legacy WireGuard outbound не содержит peer_public_key")
		}
		newPeer := map[string]interface{}{
			"address":                       freeTurnHost,
			"port":                          freeTurnPort,
			"public_key":                    publicKey,
			"allowed_ips":                   []interface{}{"0.0.0.0/0"},
			"persistent_keepalive_interval": 25,
		}
		for _, key := range []string{"pre_shared_key", "reserved"} {
			if v, exists := old[key]; exists {
				newPeer[key] = v
			}
		}
		peers = []interface{}{newPeer}
	}
	ep["peers"] = peers
	return ep, nil
}

func normalizeJSONProxyTargets(outbounds, endpoints []interface{}) error {
	for _, raw := range outbounds {
		m, ok := raw.(map[string]interface{})
		if !ok {
			return fmt.Errorf("outbound должен быть JSON object")
		}
		typ, _ := m["type"].(string)
		if typ == "" {
			return fmt.Errorf("outbound без type")
		}
		if typ == "direct" || typ == "block" || typ == "selector" || typ == "urltest" {
			continue
		}
		if original, ok := m["server"].(string); ok && original != "" && original != freeTurnHost {
			ensureTLSServerName(m, original)
		}
		if _, ok := m["server"]; ok {
			m["server"] = freeTurnHost
			m["server_port"] = freeTurnPort
		}
	}
	for _, raw := range endpoints {
		m, ok := raw.(map[string]interface{})
		if !ok {
			return fmt.Errorf("endpoint должен быть JSON object")
		}
		typ, _ := m["type"].(string)
		if typ == "wireguard" {
			peers, _ := m["peers"].([]interface{})
			if len(peers) != 1 {
				return fmt.Errorf("WireGuard endpoint должен содержать ровно один peer для FreeTurn")
			}
			peer, ok := peers[0].(map[string]interface{})
			if !ok {
				return fmt.Errorf("WireGuard peer должен быть JSON object")
			}
			peer["address"] = freeTurnHost
			peer["port"] = freeTurnPort
		}
	}
	return nil
}

func ensureTLSServerName(outbound map[string]interface{}, originalHost string) {
	tls, ok := outbound["tls"].(map[string]interface{})
	if !ok {
		return
	}
	if enabled, exists := tls["enabled"].(bool); exists && !enabled {
		return
	}
	if s, _ := tls["server_name"].(string); strings.TrimSpace(s) == "" {
		tls["server_name"] = originalHost
	}
}

// ---- URI --------------------------------------------------------------------

func buildFromURI(uri string, params ConnectParams) ([]byte, error) {
	outbound, err := parseProxyURI(uri)
	if err != nil {
		return nil, fmt.Errorf("ошибка парсинга URI: %w", err)
	}
	return assembleConfig([]interface{}{outbound}, nil, "1.1.1.1", 1400, params)
}

func parseProxyURI(uri string) (map[string]interface{}, error) {
	uri = strings.TrimSpace(uri)
	if strings.HasPrefix(strings.ToLower(uri), "ss://") {
		return parseShadowsocksURI(uri)
	}
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("невалидный URI: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return nil, fmt.Errorf("URI не содержит host")
	}
	if p := u.Port(); p != "" {
		v, err := strconv.Atoi(p)
		if err != nil || v < 1 || v > 65535 {
			return nil, fmt.Errorf("невалидный port %q", p)
		}
	}
	q := u.Query()

	switch scheme {
	case "vless":
		uuid := u.User.Username()
		if uuid == "" {
			return nil, fmt.Errorf("VLESS UUID пуст")
		}
		return buildVLESSOutbound(uuid, host, q)
	case "trojan":
		password, _ := u.User.Password()
		if password == "" {
			password = u.User.Username()
		}
		if password == "" {
			return nil, fmt.Errorf("Trojan password пуст")
		}
		return buildTrojanOutbound(password, host, q)
	case "hysteria2", "hy2":
		password, _ := url.PathUnescape(u.User.Username())
		if password == "" {
			return nil, fmt.Errorf("Hysteria2 password пуст")
		}
		return buildHysteria2Outbound(password, host, q)
	case "tuic":
		password, _ := u.User.Password()
		if u.User.Username() == "" || password == "" {
			return nil, fmt.Errorf("TUIC требует uuid:password")
		}
		return buildTUICOutbound(u.User.Username(), password, host, q)
	default:
		return nil, fmt.Errorf("неподдерживаемый протокол: %s", scheme)
	}
}

func buildVLESSOutbound(uuid, originalHost string, q url.Values) (map[string]interface{}, error) {
	out := map[string]interface{}{
		"type":        "vless",
		"tag":         "proxy",
		"server":      freeTurnHost,
		"server_port": freeTurnPort,
		"uuid":        uuid,
	}
	if flow := q.Get("flow"); flow != "" {
		out["flow"] = flow
	}
	if err := addTLS(out, q, originalHost); err != nil {
		return nil, err
	}
	if err := addTransport(out, q); err != nil {
		return nil, err
	}
	return out, nil
}

func buildTrojanOutbound(password, originalHost string, q url.Values) (map[string]interface{}, error) {
	out := map[string]interface{}{
		"type":        "trojan",
		"tag":         "proxy",
		"server":      freeTurnHost,
		"server_port": freeTurnPort,
		"password":    password,
	}
	if err := addTLS(out, q, originalHost); err != nil {
		return nil, err
	}
	if err := addTransport(out, q); err != nil {
		return nil, err
	}
	return out, nil
}

func parseShadowsocksURI(raw string) (map[string]interface{}, error) {
	body := strings.TrimSpace(raw[len("ss://"):])
	if i := strings.IndexByte(body, '#'); i >= 0 {
		body = body[:i]
	}
	if body == "" {
		return nil, fmt.Errorf("пустой Shadowsocks URI")
	}

	var userInfo, hostPort string
	if at := strings.LastIndex(body, "@"); at >= 0 {
		userInfo = body[:at]
		hostPort = body[at+1:]
	} else {
		decoded, err := decodeBase64Flexible(body)
		if err != nil {
			return nil, fmt.Errorf("legacy Shadowsocks URI: %w", err)
		}
		at := strings.LastIndex(decoded, "@")
		if at < 0 {
			return nil, fmt.Errorf("legacy Shadowsocks URI не содержит @")
		}
		userInfo = decoded[:at]
		hostPort = decoded[at+1:]
	}

	method, password, err := decodeSSUserInfo(userInfo)
	if err != nil {
		return nil, err
	}
	if strings.Contains(hostPort, "/?") {
		hostPort = strings.SplitN(hostPort, "/?", 2)[0]
	}
	u, err := url.Parse("ss://" + hostPort)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("невалидный Shadowsocks host:port %q", hostPort)
	}
	if p := u.Port(); p != "" {
		v, err := strconv.Atoi(p)
		if err != nil || v < 1 || v > 65535 {
			return nil, fmt.Errorf("невалидный Shadowsocks port %q", p)
		}
	}
	return map[string]interface{}{
		"type":        "shadowsocks",
		"tag":         "proxy",
		"server":      freeTurnHost,
		"server_port": freeTurnPort,
		"method":      method,
		"password":    password,
	}, nil
}

func decodeSSUserInfo(raw string) (string, string, error) {
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return "", "", fmt.Errorf("Shadowsocks userinfo decode: %w", err)
	}
	if !strings.Contains(decoded, ":") {
		decoded, err = decodeBase64Flexible(decoded)
		if err != nil {
			return "", "", fmt.Errorf("Shadowsocks userinfo base64: %w", err)
		}
	}
	parts := strings.SplitN(decoded, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("невалидный Shadowsocks userinfo: ожидается method:password")
	}
	return parts[0], parts[1], nil
}

func decodeBase64Flexible(s string) (string, error) {
	s = strings.TrimSpace(s)
	encodings := []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	}
	var lastErr error
	for _, enc := range encodings {
		b, err := enc.DecodeString(s)
		if err == nil {
			return string(b), nil
		}
		lastErr = err
	}
	return "", lastErr
}

func buildHysteria2Outbound(password, originalHost string, q url.Values) (map[string]interface{}, error) {
	out := map[string]interface{}{
		"type":        "hysteria2",
		"tag":         "proxy",
		"server":      freeTurnHost,
		"server_port": freeTurnPort,
		"password":    password,
		"tls":         tlsForQUIC(q, originalHost),
	}
	if obfs := q.Get("obfs"); obfs != "" {
		out["obfs"] = map[string]interface{}{
			"type":     obfs,
			"password": q.Get("obfs-password"),
		}
	}
	return out, nil
}

func buildTUICOutbound(uuid, password, originalHost string, q url.Values) (map[string]interface{}, error) {
	out := map[string]interface{}{
		"type":        "tuic",
		"tag":         "proxy",
		"server":      freeTurnHost,
		"server_port": freeTurnPort,
		"uuid":        uuid,
		"password":    password,
		"tls":         tlsForQUIC(q, originalHost),
	}
	if cc := q.Get("congestion_control"); cc != "" {
		switch cc {
		case "cubic", "new_reno", "bbr":
			out["congestion_control"] = cc
		default:
			return nil, fmt.Errorf("неподдерживаемый TUIC congestion_control %q", cc)
		}
	}
	return out, nil
}

func tlsForQUIC(q url.Values, originalHost string) map[string]interface{} {
	sni := q.Get("sni")
	if sni == "" {
		sni = originalHost
	}
	tls := map[string]interface{}{"enabled": true, "server_name": sni}
	if queryBool(q, "allowInsecure") || queryBool(q, "insecure") {
		tls["insecure"] = true
	}
	return tls
}

func addTLS(out map[string]interface{}, q url.Values, originalHost string) error {
	security := strings.ToLower(q.Get("security"))
	if security == "" || security == "none" {
		return nil
	}
	if security != "tls" && security != "reality" {
		return fmt.Errorf("неподдерживаемый security=%q", security)
	}
	sni := q.Get("sni")
	if sni == "" {
		sni = originalHost
	}
	tls := map[string]interface{}{"enabled": true, "server_name": sni}
	if fp := q.Get("fp"); fp != "" {
		tls["utls"] = map[string]interface{}{"enabled": true, "fingerprint": fp}
	}
	if security == "reality" {
		pbk := q.Get("pbk")
		if pbk == "" {
			return fmt.Errorf("Reality требует pbk")
		}
		reality := map[string]interface{}{"enabled": true, "public_key": pbk}
		if sid := q.Get("sid"); sid != "" {
			reality["short_id"] = sid
		}
		tls["reality"] = reality
	}
	if queryBool(q, "allowInsecure") || queryBool(q, "insecure") {
		tls["insecure"] = true
	}
	out["tls"] = tls
	return nil
}

func queryBool(q url.Values, key string) bool {
	v := strings.ToLower(strings.TrimSpace(q.Get(key)))
	return v == "1" || v == "true" || v == "yes"
}

func addTransport(out map[string]interface{}, q url.Values) error {
	tp := strings.ToLower(q.Get("type"))
	if tp == "" || tp == "tcp" {
		return nil
	}
	transport := map[string]interface{}{}
	switch tp {
	case "ws":
		transport["type"] = "ws"
		if host := q.Get("host"); host != "" {
			transport["headers"] = map[string]interface{}{"Host": host}
		}
		if path := q.Get("path"); path != "" {
			transport["path"] = path
		}
	case "grpc":
		transport["type"] = "grpc"
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
	default:
		return fmt.Errorf("неподдерживаемый transport type=%q", tp)
	}
	out["transport"] = transport
	return nil
}

// ---- Assembly ---------------------------------------------------------------

func assembleConfig(outbounds []interface{}, endpoints []interface{}, dnsRemote string, tunMTU int, params ConnectParams) ([]byte, error) {
	if tunMTU < 576 {
		tunMTU = 576
	}
	if tunMTU > 1500 {
		tunMTU = 1500
	}

	if err := ensureDirectOutbound(&outbounds); err != nil {
		return nil, err
	}
	proxyTag, err := ensureProxyTag(outbounds, endpoints)
	if err != nil {
		return nil, err
	}
	if err := validateTags(outbounds, endpoints); err != nil {
		return nil, err
	}

	rules := []interface{}{
		map[string]interface{}{"action": "sniff", "timeout": "300ms"},
		map[string]interface{}{"protocol": "dns", "action": "hijack-dns"},
		map[string]interface{}{
			"process_name": freeturnBypassProcessNames(),
			"action":       "route",
			"outbound":     "direct",
		},
		map[string]interface{}{"ip_is_private": true, "action": "route", "outbound": "direct"},
	}

	var ruleSets []interface{}
	if params.BypassRu {
		ruTags := []string{"ru-domains"}
		if geoPath := findGeoIPRuSRS(); geoPath != "" {
			ruTags = append([]string{"geoip-ru"}, ruTags...)
			ruleSets = append(ruleSets, map[string]interface{}{
				"type": "local", "tag": "geoip-ru", "format": "binary", "path": geoPath,
			})
		}
		ruleSets = append(ruleSets, map[string]interface{}{
			"type": "inline",
			"tag":  "ru-domains",
			"rules": []interface{}{
				map[string]interface{}{"domain_suffix": []string{".ru", ".su", ".xn--p1ai"}},
			},
		})
		rules = append(rules, map[string]interface{}{
			"rule_set": ruTags,
			"action":   "route",
			"outbound": "direct",
		})
	}

	var dnsRules []interface{}
	if params.BypassRu {
		dnsRules = append(dnsRules, map[string]interface{}{
			"rule_set": "ru-domains",
			"action":   "route",
			"server":   "dns-local",
		})
	}

	tunInbound := map[string]interface{}{
		"type":           "tun",
		"tag":            "tun-in",
		"interface_name": singTunName,
		"address":        []string{"172.19.0.1/30"},
		"mtu":            tunMTU,
		"auto_route":     true,
		"strict_route":   true,
		"stack":          "mixed",
		"dns_mode":       "hijack",
	}
	if goruntime.GOOS == "linux" {
		// Recommended by sing-box for Linux TUN performance and route handling.
		tunInbound["auto_redirect"] = true
	}

	dnsConfig := map[string]interface{}{
		"servers": []interface{}{
			map[string]interface{}{
				"type": "udp", "tag": "dns-remote", "server": dnsRemote, "detour": proxyTag,
			},
			map[string]interface{}{"type": "local", "tag": "dns-local"},
		},
		"final":    "dns-remote",
		"strategy": "ipv4_only",
	}
	if len(dnsRules) > 0 {
		dnsConfig["rules"] = dnsRules
	}

	routeConfig := map[string]interface{}{
		"rules":                   rules,
		"find_process":            true,
		"final":                   proxyTag,
		"auto_detect_interface":   true,
		"default_domain_resolver": map[string]interface{}{"server": "dns-local"},
	}
	if len(ruleSets) > 0 {
		routeConfig["rule_set"] = ruleSets
	}

	config := map[string]interface{}{
		"log": map[string]interface{}{
			"level":     "info",
			"timestamp": true,
		},
		"dns":       dnsConfig,
		"inbounds":  []interface{}{tunInbound},
		"outbounds": outbounds,
		"route":     routeConfig,
	}
	if len(endpoints) > 0 {
		config["endpoints"] = endpoints
	}
	return json.MarshalIndent(config, "", "  ")
}

func ensureDirectOutbound(outbounds *[]interface{}) error {
	for _, raw := range *outbounds {
		m, ok := raw.(map[string]interface{})
		if !ok {
			return fmt.Errorf("outbound должен быть JSON object")
		}
		tag, _ := m["tag"].(string)
		if tag != "direct" {
			continue
		}
		typ, _ := m["type"].(string)
		if typ != "direct" {
			return fmt.Errorf("зарезервированный tag direct занят outbound type=%q", typ)
		}
		return nil
	}
	*outbounds = append(*outbounds, map[string]interface{}{"type": "direct", "tag": "direct"})
	return nil
}

func ensureProxyTag(outbounds, endpoints []interface{}) (string, error) {
	for _, list := range [][]interface{}{endpoints, outbounds} {
		for _, raw := range list {
			m, ok := raw.(map[string]interface{})
			if !ok {
				return "", fmt.Errorf("proxy item должен быть JSON object")
			}
			if tag, _ := m["tag"].(string); tag == "proxy" {
				if typ, _ := m["type"].(string); typ == "direct" || typ == "block" || typ == "" {
					return "", fmt.Errorf("tag proxy назначен недопустимому type=%q", typ)
				}
				return "proxy", nil
			}
		}
	}

	for _, list := range [][]interface{}{endpoints, outbounds} {
		for _, raw := range list {
			m, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			typ, _ := m["type"].(string)
			if typ == "" || typ == "direct" || typ == "block" {
				continue
			}
			tag, _ := m["tag"].(string)
			if tag == "" {
				m["tag"] = "proxy"
				return "proxy", nil
			}
			return tag, nil
		}
	}
	return "", fmt.Errorf("не найден proxy outbound/endpoint")
}

func validateTags(outbounds, endpoints []interface{}) error {
	seen := map[string]string{}
	reserved := map[string]bool{"tun-in": true, "dns-local": true, "dns-remote": true}
	for kind, list := range map[string][]interface{}{"outbound": outbounds, "endpoint": endpoints} {
		for _, raw := range list {
			m, ok := raw.(map[string]interface{})
			if !ok {
				return fmt.Errorf("%s должен быть JSON object", kind)
			}
			typ, _ := m["type"].(string)
			if typ == "" {
				return fmt.Errorf("%s без type", kind)
			}
			tag, _ := m["tag"].(string)
			if tag == "" {
				continue
			}
			if reserved[tag] {
				return fmt.Errorf("tag %q зарезервирован FTurn", tag)
			}
			if previous, exists := seen[tag]; exists {
				return fmt.Errorf("дублирующий tag %q (%s и %s)", tag, previous, kind)
			}
			seen[tag] = kind
		}
	}
	return nil
}
