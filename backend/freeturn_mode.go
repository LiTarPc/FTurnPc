package backend

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	freeTurnModeTCP = "tcp"
	freeTurnModeUDP = "udp"
)

// ResolveFreeTurnMode chooses the FreeTurn backend relay mode. This is NOT the
// TURN transport (-transport). Both sides of FreeTurn must use the same -mode.
func ResolveFreeTurnMode(profile *ProfileData) (string, error) {
	if profile == nil {
		return "", fmt.Errorf("profile is nil")
	}

	explicit := strings.ToLower(strings.TrimSpace(profile.Mode))
	if explicit != "" && explicit != freeTurnModeTCP && explicit != freeTurnModeUDP {
		return "", fmt.Errorf("неподдерживаемый FreeTurn mode %q: ожидается tcp или udp", profile.Mode)
	}

	inferred, inferErr := inferFreeTurnMode(profile)
	if explicit != "" {
		if inferErr == nil && inferred != explicit {
			return "", fmt.Errorf("FreeTurn mode %q конфликтует с протоколом sb, для него требуется %q", explicit, inferred)
		}
		return explicit, nil
	}
	if inferErr != nil {
		return "", inferErr
	}
	return inferred, nil
}

func inferFreeTurnMode(profile *ProfileData) (string, error) {
	switch DetectConfigType(profile.SB, profile.WGConfig) {
	case ConfigTypeWG:
		return freeTurnModeUDP, nil
	case ConfigTypeURI:
		s, err := rawSBString(profile.SB)
		if err != nil {
			return "", err
		}
		i := strings.Index(s, "://")
		if i <= 0 {
			return "", fmt.Errorf("невалидный URI протокола")
		}
		return modeForProxyType(strings.ToLower(s[:i]))
	case ConfigTypeSingboxJSON:
		var cfg struct {
			Outbounds []map[string]interface{} `json:"outbounds"`
			Endpoints []map[string]interface{} `json:"endpoints"`
		}
		if err := json.Unmarshal(profile.SB, &cfg); err != nil {
			return "", fmt.Errorf("невалидный sing-box JSON: %w", err)
		}

		// Prefer the explicit proxy tag, then the first non-direct endpoint/outbound.
		for _, list := range [][]map[string]interface{}{cfg.Endpoints, cfg.Outbounds} {
			for _, item := range list {
				if tag, _ := item["tag"].(string); tag == "proxy" {
					typ, _ := item["type"].(string)
					return modeForProxyType(typ)
				}
			}
		}
		for _, list := range [][]map[string]interface{}{cfg.Endpoints, cfg.Outbounds} {
			for _, item := range list {
				typ, _ := item["type"].(string)
				if typ == "" || typ == "direct" || typ == "block" {
					continue
				}
				return modeForProxyType(typ)
			}
		}
		return "", fmt.Errorf("sing-box JSON не содержит транспортного outbound/endpoint")
	default:
		return "", fmt.Errorf("невозможно определить FreeTurn mode: неизвестный формат sb/wg")
	}
}

func modeForProxyType(proxyType string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(proxyType)) {
	case "vless", "vmess", "trojan", "shadowsocks", "ss", "anytls", "shadowtls", "http", "socks":
		return freeTurnModeTCP, nil
	case "wireguard", "hysteria", "hysteria2", "hy2", "tuic":
		return freeTurnModeUDP, nil
	default:
		return "", fmt.Errorf("неизвестно, какой FreeTurn mode нужен для sing-box type %q", proxyType)
	}
}

func rawSBString(raw json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s), nil
	}
	s = strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return "", fmt.Errorf("поле sb пустое")
	}
	return strings.Trim(s, `"`), nil
}
