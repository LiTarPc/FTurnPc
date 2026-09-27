package backend

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

var tunNameFallbackSeq atomic.Uint64

// A new Windows adapter name avoids a stale Wintun device left by a forced
// sing-box exit. Other platforms keep the established interface name.
func newSessionTunName() string {
	if runtime.GOOS != "windows" {
		return singTunName
	}
	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err == nil {
		return "fturn-" + hex.EncodeToString(suffix[:])
	}
	return fmt.Sprintf("fturn-%x", uint64(time.Now().UnixNano())+tunNameFallbackSeq.Add(1))
}

func isTunAdapterCollision(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "create adapter: cannot create a file when that file already exists") &&
		strings.Contains(msg, "open existing adapter: element not found")
}

// replaceSessionTunName changes only the generated TUN inbound before retrying
// sing-box. The failed process has already exited by the time this is called.
func replaceSessionTunName(path, name string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse sing-box config: %w", err)
	}
	inbounds, ok := cfg["inbounds"].([]interface{})
	if !ok {
		return fmt.Errorf("sing-box config has no inbounds")
	}
	found := false
	for _, raw := range inbounds {
		inbound, ok := raw.(map[string]interface{})
		if ok && inbound["type"] == "tun" {
			inbound["interface_name"] = name
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("sing-box config has no TUN inbound")
	}
	updated, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, updated, 0o600)
}
