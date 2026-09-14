package backend

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestNormalizeBypassApp(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{`steam.exe`, `steam.exe`, true},
		{`  discord.exe  `, `discord.exe`, true},
		{`"C:\\Program Files (x86)\\Steam\\steam.exe"`, `steam.exe`, true},
		{`C:/Games/Game/game.exe`, `game.exe`, true},
		{``, ``, false},
		{`   `, ``, false},
		{`.`, ``, false},
		{`..`, ``, false},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := normalizeBypassApp(tt.in)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("normalizeBypassApp(%q) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestNormalizeBypassApps_DeduplicatesCaseInsensitively(t *testing.T) {
	got := normalizeBypassApps([]string{
		"steam.exe",
		"STEAM.EXE",
		`C:\\Apps\\Discord.exe`,
		"discord.exe",
		"browser.exe",
	})
	want := []string{"steam.exe", "Discord.exe", "browser.exe"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeBypassApps() = %#v, want %#v", got, want)
	}
}

func TestApplyProcessBypassApps_IsFirstRouteRule(t *testing.T) {
	input := []byte(`{
  "route": {
    "rules": [
      {"action":"sniff"},
      {"protocol":"dns","action":"hijack-dns"},
      {"process_name":["freeturnclient.exe"],"action":"route","outbound":"direct"}
    ],
    "final":"proxy"
  }
}`)

	out, err := ApplyProcessBypassApps(input, []string{"steam.exe", `C:\\Apps\\Discord.exe`})
	if err != nil {
		t.Fatalf("ApplyProcessBypassApps: %v", err)
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("generated JSON: %v", err)
	}
	route, ok := cfg["route"].(map[string]interface{})
	if !ok {
		t.Fatal("route missing")
	}
	rules, ok := route["rules"].([]interface{})
	if !ok || len(rules) != 4 {
		t.Fatalf("route.rules = %#v", route["rules"])
	}
	first, ok := rules[0].(map[string]interface{})
	if !ok {
		t.Fatalf("first rule = %#v", rules[0])
	}
	if first["action"] != "route" || first["outbound"] != "direct" {
		t.Fatalf("first bypass rule = %#v", first)
	}
	names, ok := first["process_name"].([]interface{})
	if !ok {
		t.Fatalf("process_name = %#v", first["process_name"])
	}
	want := []interface{}{"steam.exe", "Discord.exe"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("process_name = %#v, want %#v", names, want)
	}

	second := rules[1].(map[string]interface{})
	if second["action"] != "sniff" {
		t.Fatalf("original first rule was not preserved after bypass rule: %#v", second)
	}
	third := rules[2].(map[string]interface{})
	if third["action"] != "hijack-dns" {
		t.Fatalf("DNS hijack order changed unexpectedly: %#v", third)
	}
}

func TestApplyProcessBypassApps_EmptyListDoesNotChangeConfig(t *testing.T) {
	input := []byte(`{"route":{"rules":[]}}`)
	out, err := ApplyProcessBypassApps(input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(input) {
		t.Fatalf("empty bypass list changed config: %s", out)
	}
}
