package backend

import (
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
)

var freeturnExecutableNames = []string{
	"freeturnclient",
	"client-windows-amd64",
	"client",
}

func freeturnCandidateNames() []string {
	if goruntime.GOOS == "windows" {
		return []string{
			"freeturnclient.exe",
			"client-windows-amd64.exe",
			"client.exe",
			"freeturnclient",
			"client-windows-amd64",
			"client",
		}
	}
	return append([]string(nil), freeturnExecutableNames...)
}

// freeturnBypassProcessNames returns every executable basename that getFreeturnPath
// can select. sing-box uses this list to keep the transport process outside TUN,
// preventing a routing loop back into 127.0.0.1:9000.
func freeturnBypassProcessNames() []string {
	return freeturnCandidateNames()
}

// getFreeturnPath determines the FreeTurn client executable path.
func getFreeturnPath() string {
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)

	for _, exeName := range freeturnCandidateNames() {
		path1 := filepath.Join(dir, "assets", "freeturn", exeName)
		if _, err := os.Stat(path1); err == nil {
			return path1
		}
		path2 := filepath.Join(dir, exeName)
		if _, err := os.Stat(path2); err == nil {
			return path2
		}
		if path3, err := exec.LookPath(exeName); err == nil {
			return path3
		}
	}

	defaultName := "freeturnclient"
	if goruntime.GOOS == "windows" {
		defaultName = "freeturnclient.exe"
	}
	return filepath.Join(dir, defaultName)
}
