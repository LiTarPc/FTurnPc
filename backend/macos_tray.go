//go:build darwin

package backend

func startTray(_ []byte, _, _, _ func()) {}
func setTrayVisible(_ bool)               {}
func setTrayStatus(_ bool, _, _ int64, _ int32) {}
