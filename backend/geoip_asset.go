package backend

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// installEmbeddedGeoIPRuSRS materializes the embedded RU rule-set into the
// persistent config directory. sing-box local rule_sets require a filesystem
// path, so keeping the source in the executable makes dev and release builds
// behave identically without depending on build/bin asset copying.
func installEmbeddedGeoIPRuSRS(data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("embedded geoip-ru.srs is empty")
	}

	dst := filepath.Join(configDir(), "geoip-ru.srs")
	if existing, err := os.ReadFile(dst); err == nil && bytes.Equal(existing, data) {
		return dst, nil
	}

	tmp, err := os.CreateTemp(configDir(), "geoip-ru-*.srs.tmp")
	if err != nil {
		return "", fmt.Errorf("create geoip-ru temp file: %w", err)
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		return "", fmt.Errorf("chmod geoip-ru temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return "", fmt.Errorf("write geoip-ru temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return "", fmt.Errorf("sync geoip-ru temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close geoip-ru temp file: %w", err)
	}

	_ = os.Remove(dst)
	if err := os.Rename(tmpPath, dst); err != nil {
		return "", fmt.Errorf("install geoip-ru.srs: %w", err)
	}
	_ = os.Chmod(dst, 0o600)
	ok = true
	return dst, nil
}
