package backend

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestLocalProxyServer_LifecycleAndProxying(t *testing.T) {
	// 1. Создаем тестовый Эхо-сервер (целевой сервер назначения)
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start echo server: %v", err)
	}
	defer echoListener.Close()

	echoAddr := echoListener.Addr().String()
	go func() {
		for {
			conn, err := echoListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()

	// 2. Запускаем локальный SOCKS5 & HTTP сервер
	proxy := NewLocalProxyServer(true)
	socksAddr := "127.0.0.1:10580"
	httpAddr := "127.0.0.1:10581"

	if err := proxy.Start(socksAddr, httpAddr); err != nil {
		t.Fatalf("Failed to start LocalProxyServer: %v", err)
	}
	defer proxy.Stop()

	// Даем 100мс на запуск слушателей
	time.Sleep(100 * time.Millisecond)

	// 3. Тест протокола SOCKS5 Handshake & Relay
	t.Run("SOCKS5 Relay", func(t *testing.T) {
		conn, err := net.Dial("tcp", socksAddr)
		if err != nil {
			t.Fatalf("Failed to connect to SOCKS5 proxy: %v", err)
		}
		defer conn.Close()

		// SOCKS5 greeting: VER=5, NMETHODS=1, METHOD=0 (No Auth)
		_, err = conn.Write([]byte{0x05, 0x01, 0x00})
		if err != nil {
			t.Fatalf("Failed to write SOCKS5 greeting: %v", err)
		}

		resp := make([]byte, 2)
		if _, err := io.ReadFull(conn, resp); err != nil || resp[0] != 0x05 || resp[1] != 0x00 {
			t.Fatalf("Invalid SOCKS5 greeting response: %v", resp)
		}

		// SOCKS5 request: VER=5, CMD=1 (CONNECT), RSV=0, ATYP=1 (IPv4)
		echoHost, echoPortStr, _ := net.SplitHostPort(echoAddr)
		var port int
		fmt.Sscanf(echoPortStr, "%d", &port)

		ip := net.ParseIP(echoHost).To4()
		req := []byte{0x05, 0x01, 0x00, 0x01, ip[0], ip[1], ip[2], ip[3], byte(port >> 8), byte(port)}
		if _, err := conn.Write(req); err != nil {
			t.Fatalf("Failed to write SOCKS5 connect request: %v", err)
		}

		reply := make([]byte, 10)
		if _, err := io.ReadFull(conn, reply); err != nil || reply[1] != 0x00 {
			t.Fatalf("SOCKS5 connect failed, reply: %v", reply)
		}

		// Отправляем данные и проверяем эхо-ответ
		testPayload := "Hello SOCKS5 FreeTurn!"
		if _, err := conn.Write([]byte(testPayload)); err != nil {
			t.Fatalf("Failed to send payload: %v", err)
		}

		buf := make([]byte, len(testPayload))
		if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != testPayload {
			t.Fatalf("Echo mismatch: expected %q, got %q", testPayload, string(buf))
		}
	})

	// 4. Тест HTTP CONNECT Proxy
	t.Run("HTTP CONNECT Proxy", func(t *testing.T) {
		proxyURL, _ := url.Parse("http://" + httpAddr)
		client := &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyURL(proxyURL),
			},
			Timeout: 5 * time.Second,
		}

		conn, err := net.Dial("tcp", httpAddr)
		if err != nil {
			t.Fatalf("Failed to connect to HTTP proxy: %v", err)
		}
		defer conn.Close()

		connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", echoAddr, echoAddr)
		if _, err := conn.Write([]byte(connectReq)); err != nil {
			t.Fatalf("Failed to write HTTP CONNECT request: %v", err)
		}

		reader := bufio.NewReader(conn)
		respLine, err := reader.ReadString('\n')
		if err != nil || !testing.Short() && len(respLine) < 12 {
			t.Fatalf("HTTP CONNECT response error: %v, line: %s", err, respLine)
		}

		testPayload := "Hello HTTP CONNECT FreeTurn!"
		if _, err := conn.Write([]byte(testPayload)); err != nil {
			t.Fatalf("Failed to send HTTP payload: %v", err)
		}

		buf := make([]byte, len(testPayload))
		if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != testPayload {
			t.Fatalf("HTTP Echo mismatch: expected %q, got %q", testPayload, string(buf))
		}

		_ = client
	})
}

func TestIsRuOrLocalDomain(t *testing.T) {
	tests := []struct {
		domain   string
		expected bool
	}{
		{"yandex.ru", true},
		{"sub.gosuslugi.ru", true},
		{"vk.com", true},
		{"localhost", true},
		{"127.0.0.1", true},
		{"google.com", false},
		{"youtube.com", false},
		{"discord.gg", false},
	}

	for _, tt := range tests {
		if got := isRuOrLocalDomain(tt.domain); got != tt.expected {
			t.Errorf("isRuOrLocalDomain(%q) = %v; expected %v", tt.domain, got, tt.expected)
		}
	}
}
