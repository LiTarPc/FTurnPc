package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
)

const freeTurnLoopbackRoute = "127.0.0.0/8"

// HardenSingboxLoopbackConfig protects the local FreeTurn hop from the TUN's
// own routing/interface auto-detection.
//
// FTurn deliberately rewrites the real proxy peer to 127.0.0.1:9000. On
// Windows, route.auto_detect_interface can otherwise bind an endpoint socket to
// the physical default interface. A UDP WireGuard endpoint then fails with
// WSAEADDRNOTAVAIL when it tries to send to loopback. Mature sing-box clients
// also exclude 127.0.0.0/8 from TUN auto routes for the same reason: localhost
// traffic must never enter the VPN route in the first place.
func HardenSingboxLoopbackConfig(data []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var cfg map[string]interface{}
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse generated sing-box config for loopback hardening: %w", err)
	}

	if rawInbounds, ok := cfg["inbounds"].([]interface{}); ok {
		for i, raw := range rawInbounds {
			in, ok := raw.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("inbounds[%d] is not an object", i)
			}
			if typ, _ := in["type"].(string); typ != "tun" {
				continue
			}
			excludes, err := appendUniqueStringField(in["route_exclude_address"], freeTurnLoopbackRoute)
			if err != nil {
				return nil, fmt.Errorf("tun route_exclude_address: %w", err)
			}
			in["route_exclude_address"] = excludes
		}
	}

	if rawOutbounds, ok := cfg["outbounds"].([]interface{}); ok {
		for i, raw := range rawOutbounds {
			out, ok := raw.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("outbounds[%d] is not an object", i)
			}
			if isFreeTurnServerTarget(out) {
				// Explicit source binding disables sing-box's inherited
				// auto_detect_interface bind for this local hop.
				out["inet4_bind_address"] = freeTurnHost
			}
		}
	}

	if rawEndpoints, ok := cfg["endpoints"].([]interface{}); ok {
		for i, raw := range rawEndpoints {
			ep, ok := raw.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("endpoints[%d] is not an object", i)
			}
			if typ, _ := ep["type"].(string); typ != "wireguard" {
				continue
			}
			if wireGuardUsesFreeTurnLoopback(ep) {
				ep["inet4_bind_address"] = freeTurnHost
			}
		}
	}

	// RU bypass is generated earlier by singbox_config.go. Normalize its domain
	// suffixes and report exactly whether the binary GeoIP rule-set was found.
	ruInfo, err := finalizeRUBypassMap(cfg)
	if err != nil {
		return nil, fmt.Errorf("RU bypass finalize: %w", err)
	}
	if ruInfo.Enabled {
		if ruInfo.GeoIP {
			log.Printf("[SB] RU bypass: geoip + domains (%s)", ruInfo.GeoPath)
		} else {
			log.Printf("[SB] WARN: RU bypass: domains only; geoip-ru.srs not loaded")
		}
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal hardened sing-box config: %w", err)
	}
	return out, nil
}

func appendUniqueStringField(raw interface{}, value string) ([]interface{}, error) {
	if raw == nil {
		return []interface{}{value}, nil
	}

	var values []interface{}
	switch v := raw.(type) {
	case string:
		values = []interface{}{v}
	case []interface{}:
		values = append([]interface{}(nil), v...)
	default:
		return nil, fmt.Errorf("unexpected type %T", raw)
	}

	for _, item := range values {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("contains non-string value %T", item)
		}
		if s == value {
			return values, nil
		}
	}
	return append(values, value), nil
}

func isFreeTurnServerTarget(item map[string]interface{}) bool {
	server, _ := item["server"].(string)
	if server != freeTurnHost {
		return false
	}
	return jsonPortEquals(item["server_port"], freeTurnPort)
}

func wireGuardUsesFreeTurnLoopback(endpoint map[string]interface{}) bool {
	peers, _ := endpoint["peers"].([]interface{})
	for _, raw := range peers {
		peer, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		address, _ := peer["address"].(string)
		if address == freeTurnHost && jsonPortEquals(peer["port"], freeTurnPort) {
			return true
		}
	}
	return false
}

func jsonPortEquals(raw interface{}, want int) bool {
	switch v := raw.(type) {
	case json.Number:
		n, err := v.Int64()
		return err == nil && n == int64(want)
	case float64:
		return v == float64(want)
	case int:
		return v == want
	case int64:
		return v == int64(want)
	default:
		return false
	}
}
