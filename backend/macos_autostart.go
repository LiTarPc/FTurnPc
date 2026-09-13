//go:build darwin

package backend

import (
	"os"
	"path/filepath"
	"text/template"
)

const launchAgentPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.pwdtt.app</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{.Exec}}</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<false/>
	<key>StandardErrorPath</key>
	<string>/tmp/pwdtt.stderr</string>
	<key>StandardOutPath</key>
	<string>/tmp/pwdtt.stdout</string>
</dict>
</plist>
`

func launchAgentPath() string {
	return filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", "com.pwdtt.app.plist")
}

func (a *App) SetAutoStart(v bool) error {
	path := launchAgentPath()
	if !v {
		_ = os.Remove(path)
		return nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	tmpl, err := template.New("plist").Parse(launchAgentPlist)
	if err != nil {
		return err
	}

	return tmpl.Execute(f, map[string]string{"Exec": exe})
}

func (a *App) GetAutoStart() bool {
	_, err := os.Stat(launchAgentPath())
	return err == nil
}
