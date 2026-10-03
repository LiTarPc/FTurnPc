package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRuCIDRsRejectsBadOrNonIPv4Input(t *testing.T) {
	good, err := parseRuCIDRs([]byte("2.56.24.0/22\r\n5.8.0.0/20\n2.56.24.0/22\n"))
	if err != nil || len(good) != 2 {
		t.Fatalf("valid CIDRs were not parsed and deduplicated: %v, %v", good, err)
	}
	for _, bad := range []string{
		"<html>error</html>\n",
		"2.56.24.1/22\n",
		"2001:db8::/32\n",
		"2.56.24.0/33\n",
	} {
		if _, err := parseRuCIDRs([]byte(bad)); err == nil {
			t.Errorf("bad RU CIDR data accepted: %q", bad)
		}
	}
	if _, err := parseRuCIDRs([]byte(strings.Repeat("x", maxRuCIDRBytes+1))); err == nil {
		t.Fatal("oversized RU CIDR data accepted")
	}
}

func TestReplaceRuRuleFilesRestoresOldFilesOnFailure(t *testing.T) {
	dir := t.TempDir()
	stage := filepath.Join(dir, "stage")
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	oldSRS := filepath.Join(dir, "geoip-ru.srs")
	oldTXT := filepath.Join(dir, "geoip-ru.txt")
	newSRS := filepath.Join(stage, "geoip-ru.srs")
	for path, content := range map[string]string{oldSRS: "old srs", oldTXT: "old txt", newSRS: "new srs"} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	missingTXT := filepath.Join(stage, "missing.txt")
	if err := replaceRuRuleFiles(stage, [][2]string{{newSRS, oldSRS}, {missingTXT, oldTXT}}); err == nil {
		t.Fatal("installation unexpectedly succeeded without a staged text file")
	}
	for path, want := range map[string]string{oldSRS: "old srs", oldTXT: "old txt"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Errorf("rollback of %s = %q, %v; want %q", path, got, err, want)
		}
	}
}
