package backend

import (
	"strings"
	"testing"
)

func TestCleanBrowserPath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`"C:\Program Files\Google\Chrome\Application\chrome.exe" -- "%1"`, `C:\Program Files\Google\Chrome\Application\chrome.exe`},
		{`C:\Program Files\Mozilla Firefox\firefox.exe -os-reinstall`, `C:\Program Files\Mozilla Firefox\firefox.exe`},
		{`"C:\Users\Test\AppData\Local\Programs\Opera\opera.exe"`, `C:\Users\Test\AppData\Local\Programs\Opera\opera.exe`},
	}

	for _, tc := range tests {
		got := cleanBrowserPath(tc.input)
		if got != tc.expected {
			t.Errorf("cleanBrowserPath(%q) = %q, expected %q", tc.input, got, tc.expected)
		}
	}
}

func TestIsIgnoredBrowser(t *testing.T) {
	if !isIgnoredBrowser(`C:\Users\User\AppData\Local\Yandex\YandexBrowser\Application\browser.exe`) {
		t.Errorf("Expected Yandex browser to be ignored")
	}
	if !isIgnoredBrowser(`D:\yandex_portable\browser.exe`) {
		t.Errorf("Expected yandex to be ignored")
	}
	if isIgnoredBrowser(`C:\Program Files\Google\Chrome\Application\chrome.exe`) {
		t.Errorf("Expected Chrome NOT to be ignored")
	}
	if isIgnoredBrowser(`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`) {
		t.Errorf("Expected Edge NOT to be ignored")
	}
}

func TestGetBrowserDisplayName(t *testing.T) {
	if name := getBrowserDisplayName(`C:\Program Files\Google\Chrome\Application\chrome.exe`); name != "Google Chrome" {
		t.Errorf("Expected 'Google Chrome', got %q", name)
	}
	if name := getBrowserDisplayName(`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`); name != "Microsoft Edge" {
		t.Errorf("Expected 'Microsoft Edge', got %q", name)
	}
	if name := getBrowserDisplayName(`C:\Program Files\Mozilla Firefox\firefox.exe`); name != "Mozilla Firefox" {
		t.Errorf("Expected 'Mozilla Firefox', got %q", name)
	}
}

func TestDetectInstalledBrowsers(t *testing.T) {
	browsers := DetectInstalledBrowsers()
	t.Logf("Found %d browsers:", len(browsers))
	for _, b := range browsers {
		t.Logf(" - [%s] %s (%s)", b.ID, b.Name, b.ExePath)
		if strings.Contains(strings.ToLower(b.Name), "yandex") || strings.Contains(strings.ToLower(b.ExePath), "yandex") {
			t.Errorf("Yandex browser must not be included: %s", b.ExePath)
		}
	}
}
