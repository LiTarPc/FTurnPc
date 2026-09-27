package backend

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectCoreAsset_ExactPlatformAndArch(t *testing.T) {
	assets := []githubReleaseAsset{
		{Name: "client-linux-amd64", BrowserDownloadURL: "linux-amd64"},
		{Name: "client-windows-386.exe", BrowserDownloadURL: "windows-386"},
		{Name: "client-windows-amd64.exe", BrowserDownloadURL: "windows-amd64"},
		{Name: "client-windows-arm64.exe", BrowserDownloadURL: "windows-arm64"},
		{Name: "server-windows-amd64.exe", BrowserDownloadURL: "server"},
	}
	got := selectCoreAsset(assets, "windows", "amd64")
	if got == nil || got.Name != "client-windows-amd64.exe" {
		t.Fatalf("selected = %#v, want client-windows-amd64.exe", got)
	}
	if got := selectCoreAsset(assets, "darwin", "amd64"); got != nil {
		t.Fatalf("unexpected darwin fallback: %#v", got)
	}
}

func TestValidateCoreDownloadURL(t *testing.T) {
	good := "https://github.com/LiTarPc/fturn-core/releases/download/v4.1.2/client-linux-amd64"
	if err := validateCoreDownloadURL(good); err != nil {
		t.Fatalf("valid URL rejected: %v", err)
	}
	bad := []string{
		"http://github.com/LiTarPc/fturn-core/releases/download/v4.1.2/client-linux-amd64",
		"https://evil.example/LiTarPc/fturn-core/releases/download/v4.1.2/client-linux-amd64",
		"https://github.com/other/repo/releases/download/v1/client-linux-amd64",
		"https://github.com/samosvalishe/free-turn-proxy/releases/download/v4.0.1/client-linux-amd64",
		"https://github.com/LiTarPc/fturn-core/archive/refs/heads/main.zip",
	}
	for _, raw := range bad {
		if err := validateCoreDownloadURL(raw); err == nil {
			t.Errorf("unsafe URL accepted: %s", raw)
		}
	}
}

func TestParseSHA256Digest(t *testing.T) {
	sum := sha256.Sum256([]byte("payload"))
	hexSum := hex.EncodeToString(sum[:])
	got, err := parseSHA256Digest("sha256:" + hexSum)
	if err != nil || got != hexSum {
		t.Fatalf("parse digest = %q, %v", got, err)
	}
	for _, bad := range []string{"", "md5:" + hexSum, "sha256:xyz", "sha256:abcd"} {
		if _, err := parseSHA256Digest(bad); err == nil {
			t.Errorf("bad digest accepted: %q", bad)
		}
	}
}

func TestCoreHasUpdate(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"v3.4.0", "v3.4.0", false},
		{"3.4.0", "v3.4.0", false},
		{"v3.3.0", "v3.4.0", true},
		{"v4.1.2", "v4.1.2", false},
		{"v4.1.2-preview", "v4.1.2", false},
		{"v4.1.1-preview", "v4.1.2", true},
		{"v4.1.3", "v4.1.2", false},
		{"Не установлен", "v3.4.0", true},
		{"Бинарный файл от 2026-09-01", "v3.4.0", true},
		{"v3.4.0", "", false},
	}
	for _, tt := range tests {
		if got := coreHasUpdate(tt.current, tt.latest); got != tt.want {
			t.Errorf("coreHasUpdate(%q,%q)=%v, want %v", tt.current, tt.latest, got, tt.want)
		}
	}
}

func TestCoreVersionRejectsGoPseudoVersion(t *testing.T) {
	if validCoreVersion("v0.0.0-20260926174015-4fd1340673bb") {
		t.Fatal("Go pseudo-version must not be displayed as a FreeTurn release")
	}
	if !validCoreVersion("v4.1.2-preview") {
		t.Fatal("preview FreeTurn version should be accepted")
	}
}

func TestParseCoreReportedVersion(t *testing.T) {
	for input, want := range map[string]string{
		"v4.1.3\n":                        "v4.1.3",
		"version=4.1.3-preview\r\n":    "v4.1.3-preview",
		"flag provided but not defined\n": "",
		"v0.0.0-20260926-4fd134\n":   "",
	} {
		if got := parseCoreReportedVersion(input); got != want {
			t.Errorf("parseCoreReportedVersion(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestNewestCoreReleaseIncludesPrereleases(t *testing.T) {
	rel, err := newestCoreRelease([]githubReleaseResponse{
		{TagName: "v3.3.1"},
		{TagName: "v4.1.2", Prerelease: true, PublishedAt: "2026-09-26T17:44:24Z"},
		{TagName: "v4.1.1", Prerelease: true},
		{TagName: "v4.2.0", Draft: true},
	})
	if err != nil || rel.TagName != "v4.1.2" {
		t.Fatalf("newestCoreRelease = %q, %v; want v4.1.2", rel.TagName, err)
	}
}

func TestExtractCoreExecutable_ZipUsesKnownClientNotFirstExe(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mal, _ := zw.Create("malware.exe")
	_, _ = mal.Write(fakeExecutable("windows", 2048))
	client, _ := zw.Create("bin/client-windows-amd64.exe")
	want := fakeExecutable("windows", 4096)
	_, _ = client.Write(want)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := extractCoreExecutable(buf.Bytes(), "https://github.com/LiTarPc/fturn-core/releases/download/v1/client-windows-amd64.zip", "windows", "amd64")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("selected wrong executable from ZIP")
	}
}

func TestExtractCoreExecutable_ZipRejectsUnknownFile(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, _ := zw.Create("random.exe")
	_, _ = f.Write(fakeExecutable("windows", 2048))
	_ = zw.Close()
	if _, err := extractCoreExecutable(buf.Bytes(), "https://github.com/LiTarPc/fturn-core/releases/download/v1/client-windows-amd64.zip", "windows", "amd64"); err == nil {
		t.Fatal("ZIP with no known client binary must be rejected")
	}
}

func TestValidateExecutableBytes(t *testing.T) {
	for _, osName := range []string{"windows", "linux", "darwin"} {
		if err := validateExecutableBytes(fakeExecutable(osName, 2048), osName); err != nil {
			t.Errorf("%s executable rejected: %v", osName, err)
		}
	}
	if err := validateExecutableBytes(bytes.Repeat([]byte{'x'}, 2048), "linux"); err == nil {
		t.Fatal("non-ELF data accepted")
	}
	if err := validateExecutableBytes([]byte("MZ"), "windows"); err == nil {
		t.Fatal("tiny executable accepted")
	}
}

func TestReadLimitedRejectsOverflow(t *testing.T) {
	if _, err := readLimited(strings.NewReader(strings.Repeat("a", 11)), 10); err == nil {
		t.Fatal("overflow must be rejected")
	}
	got, err := readLimited(strings.NewReader("12345"), 5)
	if err != nil || string(got) != "12345" {
		t.Fatalf("readLimited = %q, %v", got, err)
	}
}

func TestReplaceExecutableAtomically(t *testing.T) {
	if testing.Short() {
		t.Skip("filesystem replacement test")
	}
	if os.PathSeparator == '\\' {
		t.Skip("test fixture uses current platform executable format")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "freeturnclient")
	old := fakeExecutable("linux", 2048)
	newData := fakeExecutable("linux", 4096)
	old[100] = 1
	newData[100] = 2
	if err := os.WriteFile(target, old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceExecutableAtomically(target, newData); err != nil {
		t.Fatalf("replace: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newData) {
		t.Fatal("target does not contain new binary")
	}
	if _, err := os.Stat(target + ".old"); !os.IsNotExist(err) {
		t.Fatalf("backup should be removed, stat err=%v", err)
	}
}

func TestReplaceExecutableRejectsSymlink(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("symlink setup differs on Windows")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	link := filepath.Join(dir, "freeturnclient")
	if err := os.WriteFile(real, fakeExecutable("linux", 2048), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := replaceExecutableAtomically(link, fakeExecutable("linux", 4096)); err == nil {
		t.Fatal("symlink target must be rejected")
	}
}

func fakeExecutable(goos string, size int) []byte {
	if size < 1024 {
		size = 1024
	}
	b := make([]byte, size)
	switch goos {
	case "windows":
		copy(b, []byte("MZ"))
	case "linux":
		copy(b, []byte{0x7f, 'E', 'L', 'F'})
	case "darwin":
		copy(b, []byte{0xcf, 0xfa, 0xed, 0xfe})
	}
	return b
}
