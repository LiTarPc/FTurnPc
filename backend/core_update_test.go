package backend

import (
	"strings"
	"testing"
)

func TestCoreAssetSelection(t *testing.T) {
	assets := []githubReleaseAsset{
		{Name: "checksums.txt", BrowserDownloadURL: "https://.../checksums.txt"},
		{Name: "client-android-arm64", BrowserDownloadURL: "https://.../client-android-arm64"},
		{Name: "client-darwin-amd64", BrowserDownloadURL: "https://.../client-darwin-amd64"},
		{Name: "client-darwin-arm64", BrowserDownloadURL: "https://.../client-darwin-arm64"},
		{Name: "client-linux-amd64", BrowserDownloadURL: "https://.../client-linux-amd64"},
		{Name: "client-windows-386.exe", BrowserDownloadURL: "https://.../client-windows-386.exe"},
		{Name: "client-windows-amd64.exe", BrowserDownloadURL: "https://.../client-windows-amd64.exe"},
		{Name: "server-windows-amd64.exe", BrowserDownloadURL: "https://.../server-windows-amd64.exe"},
	}

	// Test Windows AMD64
	goos := "windows"
	goarch := "amd64"

	var selected string
	for _, asset := range assets {
		name := strings.ToLower(asset.Name)
		if strings.Contains(name, "server") || strings.HasSuffix(name, ".jar") || strings.HasSuffix(name, ".aar") || strings.HasSuffix(name, ".txt") {
			continue
		}
		isClient := strings.Contains(name, "client") || strings.Contains(name, "freeturn")
		matchOS := (goos == "windows" && (strings.Contains(name, "windows") || strings.Contains(name, "win32") || strings.Contains(name, "win64") || strings.HasSuffix(name, ".exe")))
		matchArch := (goarch == "amd64" && (strings.Contains(name, "amd64") || strings.Contains(name, "x86_64") || strings.Contains(name, "x64")))

		if isClient && matchOS && matchArch {
			selected = asset.Name
			break
		}
	}

	if selected != "client-windows-amd64.exe" {
		t.Fatalf("Expected client-windows-amd64.exe, got: %s", selected)
	}
}
