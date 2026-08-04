package backend

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// LocalProxyServer — высокопроизводительный локальный SOCKS5 и HTTP прокси-сервер для работы без прав администратора.
type LocalProxyServer struct {
	socksListener net.Listener
	httpListener  net.Listener
	bypassRu      bool
	mu            sync.Mutex
	closed        bool
}

func NewLocalProxyServer(bypassRu bool) *LocalProxyServer {
	return &LocalProxyServer{
		bypassRu: bypassRu,
	}
}

func (s *LocalProxyServer) Start(socksAddr, httpAddr string) error {
	// 1. Слушатель SOCKS5 (127.0.0.1:1080)
	socksL, err := net.Listen("tcp", socksAddr)
	if err != nil {
		return fmt.Errorf("ошибка запуска SOCKS5 на %s: %w", socksAddr, err)
	}
	s.socksListener = socksL

	// 2. Слушатель HTTP CONNECT (127.0.0.1:1081)
	httpL, err := net.Listen("tcp", httpAddr)
	if err != nil {
		_ = socksL.Close()
		return fmt.Errorf("ошибка запуска HTTP прокси на %s: %w", httpAddr, err)
	}
	s.httpListener = httpL

	log.Printf("[ProxyServer] Запущен локальный SOCKS5 на %s и HTTP на %s", socksAddr, httpAddr)

	go s.acceptSOCKS5()
	go s.acceptHTTP()

	return nil
}

func (s *LocalProxyServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.socksListener != nil {
		_ = s.socksListener.Close()
	}
	if s.httpListener != nil {
		_ = s.httpListener.Close()
	}
}

// acceptSOCKS5 обрабатывает клиенты RFC 1928 SOCKS5
func (s *LocalProxyServer) acceptSOCKS5() {
	for {
		conn, err := s.socksListener.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return
			}
			continue
		}
		go s.handleSOCKS5Conn(conn)
	}
}

func (s *LocalProxyServer) handleSOCKS5Conn(conn net.Conn) {
	defer conn.Close()

	buf := make([]byte, 256)
	if _, err := io.ReadFull(conn, buf[:2]); err != nil || buf[0] != 0x05 {
		return
	}
	numMethods := int(buf[1])
	if _, err := io.ReadFull(conn, buf[:numMethods]); err != nil {
		return
	}

	// Отправляем ответ: Без аутентификации (0x00)
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	// Читаем запрос (VER, CMD, RSV, ATYP, DST.ADDR, DST.PORT)
	if _, err := io.ReadFull(conn, buf[:4]); err != nil {
		return
	}
	cmd := buf[1]
	atyp := buf[3]

	if cmd != 0x01 { // Поддерживаем только команда CONNECT
		conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	var host string
	switch atyp {
	case 0x01: // IPv4
		if _, err := io.ReadFull(conn, buf[:4]); err != nil {
			return
		}
		host = net.IP(buf[:4]).String()
	case 0x03: // Domain name
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			return
		}
		domainLen := int(buf[0])
		if _, err := io.ReadFull(conn, buf[:domainLen]); err != nil {
			return
		}
		host = string(buf[:domainLen])
	case 0x04: // IPv6
		if _, err := io.ReadFull(conn, buf[:16]); err != nil {
			return
		}
		host = net.IP(buf[:16]).String()
	default:
		conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return
	}
	port := int(buf[0])<<8 | int(buf[1])
	targetAddr := fmt.Sprintf("%s:%d", host, port)

	targetConn, err := s.dialTarget(targetAddr)
	if err != nil {
		conn.Write([]byte{0x05, 0x04, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer targetConn.Close()

	if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, 0, 0}); err != nil {
		return
	}

	pipeConns(conn, targetConn)
}

// acceptHTTP обрабатывает HTTP CONNECT прокси-запросы браузеров
func (s *LocalProxyServer) acceptHTTP() {
	for {
		conn, err := s.httpListener.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return
			}
			continue
		}
		go s.handleHTTPConn(conn)
	}
}

func (s *LocalProxyServer) handleHTTPConn(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}

	if req.Method == "CONNECT" {
		targetConn, err := s.dialTarget(req.URL.Host)
		if err != nil {
			conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
			return
		}
		defer targetConn.Close()

		conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		pipeConns(conn, targetConn)
	} else {
		targetAddr := req.URL.Host
		if !strings.Contains(targetAddr, ":") {
			targetAddr += ":80"
		}
		targetConn, err := s.dialTarget(targetAddr)
		if err != nil {
			conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
			return
		}
		defer targetConn.Close()

		if err := req.Write(targetConn); err != nil {
			return
		}
		pipeConns(conn, targetConn)
	}
}

func (s *LocalProxyServer) dialTarget(targetAddr string) (net.Conn, error) {
	host, _, _ := net.SplitHostPort(targetAddr)
	if host == "" {
		host = targetAddr
	}

	// Если включен обход RU-ресурсов и домен из RU списка — соединяемся напрямую
	if s.bypassRu && isRuOrLocalDomain(host) {
		return net.DialTimeout("tcp", targetAddr, 7*time.Second)
	}

	d := &net.Dialer{Timeout: 7 * time.Second}
	return d.Dial("tcp", targetAddr)
}

func isRuOrLocalDomain(host string) bool {
	host = strings.ToLower(host)
	return strings.HasSuffix(host, ".ru") ||
		strings.HasSuffix(host, ".su") ||
		strings.HasSuffix(host, ".by") ||
		strings.HasSuffix(host, ".kz") ||
		strings.HasSuffix(host, "vk.com") ||
		strings.HasSuffix(host, "yandex.ru") ||
		strings.HasSuffix(host, "ya.ru") ||
		strings.HasSuffix(host, "gosuslugi.ru") ||
		host == "localhost" ||
		host == "127.0.0.1"
}

func pipeConns(c1, c2 net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = io.Copy(c1, c2)
		_ = c1.SetReadDeadline(time.Now().Add(5 * time.Second))
	}()

	go func() {
		defer wg.Done()
		_, _ = io.Copy(c2, c1)
		_ = c2.SetReadDeadline(time.Now().Add(5 * time.Second))
	}()

	wg.Wait()
}
