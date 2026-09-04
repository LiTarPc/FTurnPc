//go:build windows

package backend

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

// wintunDLL передается из main_windows.go через InitWintun
var wintunDLL []byte

var (
	activeDevice *device.Device
	activeTun    tun.Device
)

func InitWintun(dll []byte) { wintunDLL = dll }

// extractWintun извлекает embedded wintun.dll в директорию рядом с исполняемым файлом.
func extractWintun() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dst := filepath.Join(filepath.Dir(exe), "wintun.dll")
	if _, err := os.Stat(dst); err == nil {
		return nil // уже извлечён
	}
	return os.WriteFile(dst, wintunDLL, 0644)
}

func applyWGConfig(conf string, turnIPs []string, bypassRu bool, customMTU int) error {
	teardownWG()

	if err := extractWintun(); err != nil {
		return fmt.Errorf("extract wintun.dll: %w", err)
	}

	addr, mtuStr, allowedIPs, dnsServers, wgConf := parseWGConfig(conf)
	if addr == "" {
		return fmt.Errorf("Address not found in wg config")
	}

	mtu := 1300
	if customMTU >= 576 && customMTU <= 1500 {
		mtu = customMTU
	} else if mtuStr != "" {
		fmt.Sscanf(mtuStr, "%d", &mtu)
	}

	log.Printf("[WG] Creating Wintun interface %s with MTU %d...", wgIface, mtu)
	tunDev, err := tun.CreateTUN(wgIface, mtu)
	if err != nil {
		return fmt.Errorf("create TUN: %w", err)
	}
	activeTun = tunDev

	// Создание userspace WireGuard устройства
	logger := &device.Logger{
		Verbosef: func(format string, args ...interface{}) {},
		Errorf:   func(format string, args ...interface{}) { log.Printf("[WG] "+format, args...) },
	}
	dev := device.NewDevice(tunDev, conn.NewDefaultBind(), logger)
	activeDevice = dev

	if err := dev.IpcSetOperation(strings.NewReader(uapiConf(wgConf))); err != nil {
		return fmt.Errorf("IpcSet: %w", err)
	}

	if err := dev.Up(); err != nil {
		return fmt.Errorf("device up: %w", err)
	}

	// Назначение IP адреса на интерфейс
	if err := run("netsh", "interface", "ip", "set", "address",
		"name="+wgIface, "source=static", addr, "none"); err != nil {
		host, mask, _ := parseCIDR(addr)
		if host != "" {
			_ = run("netsh", "interface", "ip", "set", "address",
				"name="+wgIface, "source=static", host, mask)
		}
	}

	// Установка системного MTU на субинтерфейсе Windows TCP/IP
	log.Printf("[WG] Настройка системного MTU=%d на интерфейсе %s...", mtu, wgIface)
	if err := run("netsh", "interface", "ipv4", "set", "subinterface", wgIface, fmt.Sprintf("mtu=%d", mtu), "store=active"); err != nil {
		log.Printf("[WG] Предупреждение netsh MTU: %v", err)
	}

	// Добавление маршрутов-исключений (VK, RU CIDR, TURN) ДО добавления маршрутов по умолчанию
	applyExcludeRoutes(turnIPs, bypassRu)

	activeRoutesMu.Lock()
	ifIndex := activeIfaceIndex
	activeRoutesMu.Unlock()

	// Добавление маршрутов AllowedIPs через интерфейс WireGuard.
	// Сплит 0.0.0.0/0 на 0.0.0.0/1 + 128.0.0.0/1 гарантирует приоритет перед дефолтным шлюзом без трюков с метриками.
	var expandedIPs []string
	for _, cidr := range allowedIPs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "0.0.0.0/0" {
			expandedIPs = append(expandedIPs, "0.0.0.0/1", "128.0.0.0/1")
		} else {
			expandedIPs = append(expandedIPs, cidr)
		}
	}
	for _, cidr := range expandedIPs {
		if err := run("netsh", "interface", "ip", "add", "route", cidr, wgIface, "metric=1"); err != nil {
			log.Printf("[WG] add route %s err: %v", cidr, err)
		} else {
			log.Printf("[WG] route added: %s via %s", cidr, wgIface)
		}
	}

	// Защита от утечек DNS (применяется асинхронно, не задерживая поднятие туннеля)
	go applyDNSLeakProtection(dnsServers, ifIndex)

	log.Printf("[WG] Туннель %s поднят (userspace)", wgIface)
	return nil
}

func teardownWG() {
	teardownDNSLeakProtection()
	deleteExcludeRoutes()

	if activeDevice != nil {
		activeDevice.Close()
		activeDevice = nil
	}
	if activeTun != nil {
		activeTun.Close()
		activeTun = nil
	}
}

// uapiConf преобразует wg-setconf конфиг в UAPI протокол для device.IpcSetOperation.
func uapiConf(wgConf string) string {
	var sb strings.Builder
	inPeer := false
	for _, line := range strings.Split(wgConf, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == "[Interface]" {
			inPeer = false
			continue
		}
		if trimmed == "[Peer]" {
			if inPeer {
				sb.WriteString("\n") // пустая строка разделяет пиры
			}
			inPeer = true
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])

		switch key {
		case "privatekey":
			sb.WriteString("private_key=" + toHex(val) + "\n")
		case "listenport":
			sb.WriteString("listen_port=" + val + "\n")
		case "publickey":
			sb.WriteString("public_key=" + toHex(val) + "\n")
		case "presharedkey":
			sb.WriteString("preshared_key=" + toHex(val) + "\n")
		case "endpoint":
			sb.WriteString("endpoint=" + val + "\n")
		case "allowedips":
			for _, cidr := range strings.Split(val, ",") {
				if c := strings.TrimSpace(cidr); c != "" {
					sb.WriteString("allowed_ip=" + c + "\n")
				}
			}
		case "persistentkeepalive":
			sb.WriteString("persistent_keepalive_interval=" + val + "\n")
		}
	}
	sb.WriteString("\n")
	return sb.String()
}

// toHex преобразует Base64 ключ WireGuard в шестнадцатеричный вид (hex).
func toHex(b64 string) string {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return b64
	}
	return hex.EncodeToString(raw)
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w — %s", name, args, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runWithTimeout(timeout time.Duration, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w — %s", name, args, err, strings.TrimSpace(string(out)))
	}
	return nil
}
