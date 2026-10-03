package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type ruBypassInfo struct {
	Enabled bool
	GeoIP   bool
	GeoPath string
	Domains bool
}

// finalizeRUBypassConfig validates the generated RU split-tunnel section and
// normalizes domain_suffix values to sing-box's canonical form (without a
// leading dot). It also reports exactly which rule sources are active so the
// UI/session log can distinguish full GeoIP+domain bypass from a domain-only
// fallback.
func finalizeRUBypassConfig(data []byte, enabled bool) ([]byte, ruBypassInfo, error) {
	if !enabled {
		return data, ruBypassInfo{}, nil
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var cfg map[string]interface{}
	if err := dec.Decode(&cfg); err != nil {
		return nil, ruBypassInfo{Enabled: true}, fmt.Errorf("parse sing-box config for RU bypass: %w", err)
	}
	info, err := finalizeRUBypassMap(cfg)
	if err != nil {
		return nil, info, err
	}
	if !info.Enabled {
		return nil, info, fmt.Errorf("RU bypass enabled but ru-domains rule-set is missing")
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, info, fmt.Errorf("marshal finalized RU bypass config: %w", err)
	}
	return out, info, nil
}

// finalizeRUBypassMap is also used by the mandatory loopback-hardening pass so
// every generated config gets the same RU rule normalization before
// `sing-box check`.
func finalizeRUBypassMap(cfg map[string]interface{}) (ruBypassInfo, error) {
	var info ruBypassInfo
	route, ok := cfg["route"].(map[string]interface{})
	if !ok {
		return info, nil
	}

	rawSets, ok := route["rule_set"].([]interface{})
	if !ok {
		return info, nil
	}
	for i, raw := range rawSets {
		set, ok := raw.(map[string]interface{})
		if !ok {
			return info, fmt.Errorf("RU bypass: rule_set[%d] is not an object", i)
		}
		tag, _ := set["tag"].(string)
		switch tag {
		case "geoip-ru":
			if typ, _ := set["type"].(string); typ == "local" {
				if p, _ := set["path"].(string); strings.TrimSpace(p) != "" {
					info.GeoIP = true
					info.GeoPath = p
				}
			}
		case "ru-domains":
			info.Enabled = true
			info.Domains = true
			if rules, ok := set["rules"].([]interface{}); ok {
				for _, rr := range rules {
					rule, ok := rr.(map[string]interface{})
					if !ok {
						continue
					}
					rule["domain_suffix"] = normalizeDomainSuffixField(rule["domain_suffix"])
				}
			}
		}
	}
	return info, nil
}

func normalizeDomainSuffixField(raw interface{}) interface{} {
	normalize := func(s string) string {
		return strings.TrimPrefix(strings.TrimSpace(s), ".")
	}

	switch v := raw.(type) {
	case string:
		return normalize(v)
	case []interface{}:
		out := make([]interface{}, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				if s = normalize(s); s != "" {
					out = append(out, s)
				}
			}
		}
		return out
	default:
		return raw
	}
}
