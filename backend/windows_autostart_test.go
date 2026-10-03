//go:build windows

package backend

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestTaskPowerShellArguments(t *testing.T) {
	path := `C:\Program Files\O'Brien\FTurnPc-singbox.exe`
	quoted := psQuote(path)
	if quoted != `'C:\Program Files\O''Brien\FTurnPc-singbox.exe'` {
		t.Fatalf("unsafe PowerShell path quoting: %q", quoted)
	}
	encoded := base64.StdEncoding.EncodeToString(utf16LE("Привет " + quoted))
	bytes, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(bytes)%2 != 0 {
		t.Fatalf("invalid encoded command: %v", err)
	}
	units := make([]uint16, len(bytes)/2)
	for i := range units {
		units[i] = uint16(bytes[2*i]) | uint16(bytes[2*i+1])<<8
	}
	if got := string(utf16.Decode(units)); !strings.Contains(got, quoted) || !strings.HasPrefix(got, "Привет ") {
		t.Fatalf("encoded command changed script: %q", got)
	}
}
