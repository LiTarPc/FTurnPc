//go:build linux

package backend

import (
	"bytes"
	"image"
	"image/png"
	_ "image/png"
	_ "image/jpeg"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"

	"pwdtt-desktop/backend/tray"
)

func startTray(iconData []byte, onShow, onToggle, onQuit func()) {
	tray.OnShow = onShow
	tray.OnQuit = onQuit

	tmp := filepath.Join(os.TempDir(), "wdtt-tray-icon.png")

	if src, _, err := image.Decode(bytes.NewReader(iconData)); err == nil {
		dst := image.NewRGBA(image.Rect(0, 0, 22, 22))
		draw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
		var buf bytes.Buffer
		if err := png.Encode(&buf, dst); err == nil {
			iconData = buf.Bytes()
		}
	}

	_ = os.WriteFile(tmp, iconData, 0o644)

	tray.Init(tmp)

	go tray.GtkMain()
}

func setTrayVisible(v bool) {
	tray.SetVisible(v)
}

func setTrayStatus(connected bool, rx, tx int64, workers int32) {
	tray.SetStatus(connected, rx, tx, workers)
}
