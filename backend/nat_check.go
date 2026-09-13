package backend

import (
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

// NATResult представляет информацию об определенном типе NAT
type NATResult struct {
	NATType    string `json:"natType"`    // Full Cone NAT, Restricted, Symmetric NAT, UDP Blocked
	MappedIP   string `json:"mappedIp"`   // Внешний IP
	MappedPort int    `json:"mappedPort"` // Внешний порт
	Details    string `json:"details"`
}

// CheckNATType выполняет проверку типа NAT с использованием открытых STUN-серверов
func CheckNATType() (*NATResult, error) {
	stunServers := []string{
		"stun.l.google.com:19302",
		"stun1.l.google.com:19302",
		"stun2.l.google.com:19302",
		"stun.ekiga.net:3478",
	}

	var conn net.Conn
	var err error
	var chosenServer string

	for _, srv := range stunServers {
		conn, err = net.DialTimeout("udp", srv, 2*time.Second)
		if err == nil {
			chosenServer = srv
			break
		}
	}

	if conn == nil {
		return &NATResult{
			NATType: "UDP Blocked",
			Details: "Не удалось подключиться к STUN-серверам (UDP заблокирован)",
		}, nil
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	// STUN Binding Request Header (RFC 5389 / RFC 3489)
	req := make([]byte, 20)
	req[0] = 0x00
	req[1] = 0x01 // Binding Request
	req[2] = 0x00
	req[3] = 0x00 // Length: 0
	// Magic Cookie (0x2112A442)
	req[4] = 0x21
	req[5] = 0x12
	req[6] = 0xA4
	req[7] = 0x42
	// Transaction ID (12 bytes)
	copy(req[8:20], []byte("FTurnCheck12"))

	if _, err := conn.Write(req); err != nil {
		return &NATResult{NATType: "UDP Blocked", Details: err.Error()}, nil
	}

	resp := make([]byte, 1024)
	n, err := conn.Read(resp)
	if err != nil || n < 20 {
		return &NATResult{NATType: "UDP Filtered", Details: "STUN-сервер не ответил на запрос"}, nil
	}

	mappedIP, mappedPort, err := parseSTUNResponse(resp[:n])
	if err != nil {
		return &NATResult{NATType: "Error", Details: err.Error()}, nil
	}

	localAddr, _ := conn.LocalAddr().(*net.UDPAddr)
	if localAddr != nil && localAddr.IP.String() == mappedIP && localAddr.Port == mappedPort {
		return &NATResult{
			NATType:    "Open Internet (Без NAT)",
			MappedIP:   mappedIP,
			MappedPort: mappedPort,
			Details:    "Прямое подключение к сети",
		}, nil
	}

	// Второй тест: запрос к другому STUN-серверу для разделения Symmetric NAT и Cone NAT
	var secondConn net.Conn
	var secondServer string
	for _, srv := range stunServers {
		if srv == chosenServer {
			continue
		}
		c, err := net.DialTimeout("udp", srv, 2*time.Second)
		if err == nil {
			secondConn = c
			secondServer = srv
			break
		}
	}

	if secondConn != nil {
		defer secondConn.Close()
		_ = secondConn.SetDeadline(time.Now().Add(3 * time.Second))
		req2 := make([]byte, 20)
		req2[0] = 0x00
		req2[1] = 0x01
		req2[4] = 0x21
		req2[5] = 0x12
		req2[6] = 0xA4
		req2[7] = 0x42
		copy(req2[8:20], []byte("FTurnCheck34"))

		if _, err := secondConn.Write(req2); err == nil {
			resp2 := make([]byte, 1024)
			if n2, err := secondConn.Read(resp2); err == nil && n2 >= 20 {
				mIP2, mPort2, _ := parseSTUNResponse(resp2[:n2])
				if mIP2 != "" && (mIP2 != mappedIP || mPort2 != mappedPort) {
					return &NATResult{
						NATType:    "Symmetric NAT (Симметричный)",
						MappedIP:   mappedIP,
						MappedPort: mappedPort,
						Details:    fmt.Sprintf("Разные порты для разных серверов (%s vs %s)", chosenServer, secondServer),
					}, nil
				}
			}
		}
	}

	return &NATResult{
		NATType:    "Full Cone / Restricted NAT",
		MappedIP:   mappedIP,
		MappedPort: mappedPort,
		Details:    fmt.Sprintf("Порт сохраняется (%s:%d)", mappedIP, mappedPort),
	}, nil
}

func parseSTUNResponse(data []byte) (string, int, error) {
	if len(data) < 20 {
		return "", 0, fmt.Errorf("слишком короткий заголовок STUN")
	}
	msgLen := int(binary.BigEndian.Uint16(data[2:4]))
	if len(data) < 20+msgLen {
		return "", 0, fmt.Errorf("неполный пакет STUN")
	}

	pos := 20
	for pos+4 <= len(data) {
		attrType := binary.BigEndian.Uint16(data[pos : pos+2])
		attrLen := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
		pos += 4

		if pos+attrLen > len(data) {
			break
		}

		attrVal := data[pos : pos+attrLen]
		pos += attrLen
		if padding := attrLen % 4; padding != 0 {
			pos += 4 - padding
		}

		// 0x0001 = MAPPED-ADDRESS, 0x0020 = XOR-MAPPED-ADDRESS
		if attrType == 0x0001 && len(attrVal) >= 8 {
			family := attrVal[1]
			port := int(binary.BigEndian.Uint16(attrVal[2:4]))
			if family == 0x01 {
				ip := net.IP(attrVal[4:8]).String()
				return ip, port, nil
			}
		} else if attrType == 0x0020 && len(attrVal) >= 8 {
			family := attrVal[1]
			xport := binary.BigEndian.Uint16(attrVal[2:4]) ^ 0x2112
			if family == 0x01 {
				xip := make(net.IP, 4)
				magicCookie := []byte{0x21, 0x12, 0xA4, 0x42}
				for i := 0; i < 4; i++ {
					xip[i] = attrVal[4+i] ^ magicCookie[i]
				}
				return xip.String(), int(xport), nil
			}
		}
	}
	return "", 0, fmt.Errorf("атрибут MAPPED-ADDRESS не найден")
}
