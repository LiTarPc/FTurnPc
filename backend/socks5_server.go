//go:build windows

package backend

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/things-go/go-socks5"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

type Socks5Instance struct {
	dev      *device.Device
	listener net.Listener
	tnet     *netstack.Net
	rxBytes  int64
	txBytes  int64
	mu       sync.Mutex
	stopped  bool
}

type countingConn struct {
	net.Conn
	rx *int64
	tx *int64
}

type netstackResolver struct {
	tnet *netstack.Net
}

func (r *netstackResolver) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	// If name is already a valid IP, return it directly without DNS lookup
	if ip := net.ParseIP(name); ip != nil {
		return ctx, ip, nil
	}
	addrs, err := r.tnet.LookupContextHost(ctx, name)
	if err != nil || len(addrs) == 0 {
		return ctx, nil, fmt.Errorf("netstack DNS lookup %s: %w", name, err)
	}
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip != nil {
			return ctx, ip, nil
		}
	}
	return ctx, nil, fmt.Errorf("no valid IP found for %s", name)
}

func (c *countingConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		atomic.AddInt64(c.rx, int64(n))
	}
	return n, err
}

func (c *countingConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if n > 0 {
		atomic.AddInt64(c.tx, int64(n))
	}
	return n, err
}

func StartNetstackSocks5(wgConfig string, listenAddr string, customMTU int) (*Socks5Instance, error) {
	addrStr, mtuStr, _, dnsServers, wgConf := parseWGConfig(wgConfig)
	if addrStr == "" {
		return nil, fmt.Errorf("Address not found in wg config")
	}

	mtu := 1300
	if customMTU >= 576 && customMTU <= 1500 {
		mtu = customMTU
	} else if mtuStr != "" {
		var parsedMTU int
		if _, err := fmt.Sscanf(mtuStr, "%d", &parsedMTU); err == nil && parsedMTU >= 576 && parsedMTU <= 1500 {
			mtu = parsedMTU
		}
	}

	hostIP, _, err := parseCIDR(addrStr)
	if err != nil || hostIP == "" {
		log.Printf("[SOCKS5-WG] Warning parsing CIDR %q: %v, attempting raw IP parse", addrStr, err)
		hostIP = addrStr
	}
	localAddr, err := netip.ParseAddr(hostIP)
	if err != nil {
		return nil, fmt.Errorf("parse local IP %q: %w", hostIP, err)
	}

	var dnsAddrs []netip.Addr
	for _, dns := range dnsServers {
		if parsed, err := netip.ParseAddr(dns); err == nil {
			dnsAddrs = append(dnsAddrs, parsed)
		}
	}
	if len(dnsAddrs) == 0 {
		dnsAddrs = []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("1.0.0.1")}
	}

	log.Printf("[SOCKS5-WG] Creating netstack TUN device with IP %s, MTU %d...", localAddr, mtu)
	tunDev, tnet, err := netstack.CreateNetTUN([]netip.Addr{localAddr}, dnsAddrs, mtu)
	if err != nil {
		return nil, fmt.Errorf("CreateNetTUN: %w", err)
	}

	logger := &device.Logger{
		Verbosef: func(format string, args ...interface{}) {},
		Errorf:   func(format string, args ...interface{}) { log.Printf("[SOCKS5-WG] "+format, args...) },
	}

	dev := device.NewDevice(tunDev, conn.NewDefaultBind(), logger)

	if err := dev.IpcSetOperation(strings.NewReader(uapiConf(wgConf))); err != nil {
		dev.Close()
		return nil, fmt.Errorf("IpcSetOperation: %w", err)
	}

	if err := dev.Up(); err != nil {
		dev.Close()
		return nil, fmt.Errorf("device up: %w", err)
	}

	inst := &Socks5Instance{
		dev:  dev,
		tnet: tnet,
	}

	socksServer := socks5.NewServer(
		socks5.WithResolver(&netstackResolver{tnet: tnet}),
		socks5.WithRule(&socks5.PermitCommand{
			EnableConnect:   true,
			EnableBind:      false,
			EnableAssociate: false,
		}),
		socks5.WithDial(func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := tnet.DialContext(ctx, network, addr)
			if err != nil {
				log.Printf("[SOCKS5-WG] Dial error for %s: %v", addr, err)
				return nil, err
			}
			return &countingConn{
				Conn: conn,
				rx:   &inst.rxBytes,
				tx:   &inst.txBytes,
			}, nil
		}),
	)

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		dev.Close()
		return nil, fmt.Errorf("listen SOCKS5 on %s: %w", listenAddr, err)
	}
	inst.listener = listener

	go func() {
		log.Printf("[SOCKS5-WG] SOCKS5 server listening on %s (CONNECT only, netstack DNS)", listenAddr)
		if err := socksServer.Serve(listener); err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
			log.Printf("[SOCKS5-WG] SOCKS5 serve error: %v", err)
		}
	}()

	return inst, nil
}

func (s *Socks5Instance) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	listener := s.listener
	dev := s.dev
	s.listener = nil
	s.dev = nil
	s.mu.Unlock()

	if listener != nil {
		_ = listener.Close()
	}
	if dev != nil {
		dev.Close()
	}
	log.Printf("[SOCKS5-WG] SOCKS5 netstack instance stopped")
}

func (s *Socks5Instance) GetBytes() (rx, tx int64) {
	if s == nil {
		return 0, 0
	}
	return atomic.LoadInt64(&s.rxBytes), atomic.LoadInt64(&s.txBytes)
}
