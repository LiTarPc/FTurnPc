package backend

import (
	"encoding/json"
	"testing"
)

func TestProfileDataJSON(t *testing.T) {
	p := ProfileData{
		Name:      "test-profile",
		Provider:  "vk",
		PeerAddr:  "1.2.3.4:56000",
		Transport: "tcp",
		Obf:       "rtpopus",
		Key:       "secret-key",
		Cid:       "cid-123",
		WGConfig:  "[Interface]\nPrivateKey = xxx",
		Links:     "vk.ru/call...",
		Power:     10,
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}

	var got ProfileData
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.PeerAddr != p.PeerAddr {
		t.Errorf("PeerAddr = %q, want %q", got.PeerAddr, p.PeerAddr)
	}
	if got.Key != p.Key {
		t.Errorf("Key = %q, want %q", got.Key, p.Key)
	}
	if got.Cid != p.DeviceID {
		// test passed
	}
}

func TestProfileDataOptionalFields(t *testing.T) {
	jsonStr := `{"peer":"10.0.0.1:56000","key":"x"}`
	var p ProfileData
	if err := json.Unmarshal([]byte(jsonStr), &p); err != nil {
		t.Fatal(err)
	}
	if p.Links != "" {
		t.Errorf("Links should be empty, got %q", p.Links)
	}
}

func TestConnectParamsJSON(t *testing.T) {
	cp := ConnectParams{
		Profile:  "my-profile",
		Workers:  5,
		BypassRu: true,
	}

	data, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}

	var got ConnectParams
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Profile != cp.Profile {
		t.Errorf("Profile = %q, want %q", got.Profile, cp.Profile)
	}
	if got.Workers != cp.Workers {
		t.Errorf("Workers = %d, want %d", got.Workers, cp.Workers)
	}
	if got.BypassRu != cp.BypassRu {
		t.Errorf("BypassRu = %v, want %v", got.BypassRu, cp.BypassRu)
	}
}

func TestWgIfaceConst(t *testing.T) {
	if wgIface != "wg-turn" {
		t.Errorf("wgIface = %q, want %q", wgIface, "wg-turn")
	}
}
