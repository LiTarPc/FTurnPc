package backend

import (
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
)

// getFreeturnPath определяет путь к бинарному исполняемому файлу freeturnclient.
func getFreeturnPath() string {
	exeNames := []string{"freeturnclient", "client-windows-amd64", "client"}
	if goruntime.GOOS == "windows" {
		exeNames = []string{"freeturnclient.exe", "client-windows-amd64.exe", "client.exe", "freeturnclient", "client-windows-amd64"}
	}
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)

	for _, exeName := range exeNames {
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
