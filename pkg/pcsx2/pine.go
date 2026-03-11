// PINE IPC client for PCSX2.
//
// Connects to PCSX2's built-in PINE server via Unix socket or TCP.
// Provides memory read/write and emulator status without /proc/pid/mem.
//
// Protocol: each message is [4-byte LE size][payload].
// Payload is one or more commands; response is [4-byte LE size][status byte][results...].
// Status: 0x00 = OK, 0xFF = FAIL.
//
// Reference: pcsx2/PINE.cpp
package pcsx2

import (
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"os"
	"time"
)

// PINE IPC opcodes.
const (
	pineRead8      = 0
	pineRead16     = 1
	pineRead32     = 2
	pineRead64     = 3
	pineWrite8     = 4
	pineWrite16    = 5
	pineWrite32    = 6
	pineWrite64    = 7
	pineVersion    = 8
	pineSaveState  = 9
	pineLoadState  = 0xA
	pineTitle      = 0xB
	pineID         = 0xC
	pineUUID       = 0xD
	pineGameVer    = 0xE
	pineStatus     = 0xF
)

// PINEClient communicates with PCSX2 via the PINE IPC protocol.
type PINEClient struct {
	conn net.Conn
}

// PINEConnect connects to PCSX2's PINE server.
// Tries Unix socket first ($XDG_RUNTIME_DIR/pcsx2.sock or /tmp/pcsx2.sock),
// then falls back to TCP localhost:28011.
func PINEConnect() (*PINEClient, error) {
	// Try Unix socket paths
	sockPaths := []string{}
	if runtime := os.Getenv("XDG_RUNTIME_DIR"); runtime != "" {
		sockPaths = append(sockPaths, runtime+"/pcsx2.sock")
	}
	sockPaths = append(sockPaths, "/tmp/pcsx2.sock")

	for _, path := range sockPaths {
		conn, err := net.DialTimeout("unix", path, 2*time.Second)
		if err == nil {
			return &PINEClient{conn: conn}, nil
		}
	}

	// Fall back to TCP
	conn, err := net.DialTimeout("tcp", "localhost:28011", 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("PINE: cannot connect (tried unix sockets and tcp:28011): %w", err)
	}
	return &PINEClient{conn: conn}, nil
}

// Close closes the connection.
func (p *PINEClient) Close() error {
	return p.conn.Close()
}

// send sends a command payload and returns the response payload (after size+status).
func (p *PINEClient) send(payload []byte) ([]byte, error) {
	// Write: [4-byte LE size (includes these 4 bytes + payload)][payload]
	size := uint32(4 + len(payload))
	header := make([]byte, 4)
	binary.LittleEndian.PutUint32(header, size)

	p.conn.SetDeadline(time.Now().Add(5 * time.Second))

	if _, err := p.conn.Write(header); err != nil {
		return nil, fmt.Errorf("PINE send header: %w", err)
	}
	if _, err := p.conn.Write(payload); err != nil {
		return nil, fmt.Errorf("PINE send payload: %w", err)
	}

	// Read response: [4-byte LE size][status][data...]
	var respHeader [4]byte
	if _, err := readFull(p.conn, respHeader[:]); err != nil {
		return nil, fmt.Errorf("PINE recv header: %w", err)
	}
	respSize := binary.LittleEndian.Uint32(respHeader[:])
	if respSize < 5 || respSize > 650000 {
		return nil, fmt.Errorf("PINE: invalid response size %d", respSize)
	}

	respBody := make([]byte, respSize-4)
	if _, err := readFull(p.conn, respBody); err != nil {
		return nil, fmt.Errorf("PINE recv body: %w", err)
	}

	// First byte is status
	if respBody[0] != 0x00 {
		return nil, fmt.Errorf("PINE: command failed (status 0x%02X)", respBody[0])
	}

	return respBody[1:], nil // skip status byte
}

func readFull(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		if err != nil {
			return total + n, err
		}
		total += n
	}
	return total, nil
}

// ReadU8 reads a uint8 from EE RAM.
func (p *PINEClient) ReadU8(addr uint32) (uint8, error) {
	payload := make([]byte, 5)
	payload[0] = pineRead8
	binary.LittleEndian.PutUint32(payload[1:], addr)
	resp, err := p.send(payload)
	if err != nil {
		return 0, err
	}
	if len(resp) < 1 {
		return 0, fmt.Errorf("PINE: short response")
	}
	return resp[0], nil
}

// ReadU16 reads a little-endian uint16 from EE RAM.
func (p *PINEClient) ReadU16(addr uint32) (uint16, error) {
	payload := make([]byte, 5)
	payload[0] = pineRead16
	binary.LittleEndian.PutUint32(payload[1:], addr)
	resp, err := p.send(payload)
	if err != nil {
		return 0, err
	}
	if len(resp) < 2 {
		return 0, fmt.Errorf("PINE: short response")
	}
	return binary.LittleEndian.Uint16(resp), nil
}

// ReadU32 reads a little-endian uint32 from EE RAM.
func (p *PINEClient) ReadU32(addr uint32) (uint32, error) {
	payload := make([]byte, 5)
	payload[0] = pineRead32
	binary.LittleEndian.PutUint32(payload[1:], addr)
	resp, err := p.send(payload)
	if err != nil {
		return 0, err
	}
	if len(resp) < 4 {
		return 0, fmt.Errorf("PINE: short response")
	}
	return binary.LittleEndian.Uint32(resp), nil
}

// ReadU64 reads a little-endian uint64 from EE RAM.
func (p *PINEClient) ReadU64(addr uint32) (uint64, error) {
	payload := make([]byte, 5)
	payload[0] = pineRead64
	binary.LittleEndian.PutUint32(payload[1:], addr)
	resp, err := p.send(payload)
	if err != nil {
		return 0, err
	}
	if len(resp) < 8 {
		return 0, fmt.Errorf("PINE: short response")
	}
	return binary.LittleEndian.Uint64(resp), nil
}

// ReadF32 reads a little-endian float32 from EE RAM.
func (p *PINEClient) ReadF32(addr uint32) (float32, error) {
	v, err := p.ReadU32(addr)
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(v), nil
}

// WriteU8 writes a uint8 to EE RAM.
func (p *PINEClient) WriteU8(addr uint32, val uint8) error {
	payload := make([]byte, 6)
	payload[0] = pineWrite8
	binary.LittleEndian.PutUint32(payload[1:], addr)
	payload[5] = val
	_, err := p.send(payload)
	return err
}

// WriteU16 writes a little-endian uint16 to EE RAM.
func (p *PINEClient) WriteU16(addr uint32, val uint16) error {
	payload := make([]byte, 7)
	payload[0] = pineWrite16
	binary.LittleEndian.PutUint32(payload[1:], addr)
	binary.LittleEndian.PutUint16(payload[5:], val)
	_, err := p.send(payload)
	return err
}

// WriteU32 writes a little-endian uint32 to EE RAM.
func (p *PINEClient) WriteU32(addr uint32, val uint32) error {
	payload := make([]byte, 9)
	payload[0] = pineWrite32
	binary.LittleEndian.PutUint32(payload[1:], addr)
	binary.LittleEndian.PutUint32(payload[5:], val)
	_, err := p.send(payload)
	return err
}

// WriteU64 writes a little-endian uint64 to EE RAM.
func (p *PINEClient) WriteU64(addr uint32, val uint64) error {
	payload := make([]byte, 13)
	payload[0] = pineWrite64
	binary.LittleEndian.PutUint32(payload[1:], addr)
	binary.LittleEndian.PutUint64(payload[5:], val)
	_, err := p.send(payload)
	return err
}

// Read reads a block of bytes from EE RAM using batched Read32 commands.
// PINE doesn't have a bulk read, so we batch Read32 calls in one message.
func (p *PINEClient) Read(addr uint32, size int) ([]byte, error) {
	if size <= 0 {
		return nil, nil
	}
	// Round up to 4-byte aligned reads
	nWords := (size + 3) / 4
	// Build batched payload: multiple Read32 commands in one message
	payload := make([]byte, nWords*5)
	for i := 0; i < nWords; i++ {
		off := i * 5
		payload[off] = pineRead32
		binary.LittleEndian.PutUint32(payload[off+1:], addr+uint32(i*4))
	}
	resp, err := p.send(payload)
	if err != nil {
		return nil, err
	}
	if len(resp) < nWords*4 {
		return nil, fmt.Errorf("PINE: short bulk read response (%d < %d)", len(resp), nWords*4)
	}
	result := make([]byte, nWords*4)
	copy(result, resp[:nWords*4])
	return result[:size], nil
}

// Write writes a block of bytes to EE RAM using batched Write32 commands.
func (p *PINEClient) Write(addr uint32, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	// Pad to 4-byte alignment
	padded := data
	if len(data)%4 != 0 {
		padded = make([]byte, ((len(data)+3)/4)*4)
		copy(padded, data)
	}
	nWords := len(padded) / 4
	payload := make([]byte, nWords*9)
	for i := 0; i < nWords; i++ {
		off := i * 9
		payload[off] = pineWrite32
		binary.LittleEndian.PutUint32(payload[off+1:], addr+uint32(i*4))
		copy(payload[off+5:off+9], padded[i*4:(i+1)*4])
	}
	_, err := p.send(payload)
	return err
}

// Version returns the PCSX2 version string.
func (p *PINEClient) Version() (string, error) {
	resp, err := p.send([]byte{pineVersion})
	if err != nil {
		return "", err
	}
	if len(resp) < 4 {
		return "", fmt.Errorf("PINE: short version response")
	}
	strLen := binary.LittleEndian.Uint32(resp[:4])
	if len(resp) < int(4+strLen) {
		return "", fmt.Errorf("PINE: truncated version string")
	}
	// Trim null terminator
	s := resp[4 : 4+strLen]
	if len(s) > 0 && s[len(s)-1] == 0 {
		s = s[:len(s)-1]
	}
	return string(s), nil
}

// Status returns the emulator status: 0=Running, 1=Paused, 2=Shutdown.
func (p *PINEClient) Status() (uint32, error) {
	resp, err := p.send([]byte{pineStatus})
	if err != nil {
		return 0, err
	}
	if len(resp) < 4 {
		return 0, fmt.Errorf("PINE: short status response")
	}
	return binary.LittleEndian.Uint32(resp), nil
}

// GameTitle returns the game title.
func (p *PINEClient) GameTitle() (string, error) {
	resp, err := p.send([]byte{pineTitle})
	if err != nil {
		return "", err
	}
	if len(resp) < 4 {
		return "", fmt.Errorf("PINE: short title response")
	}
	strLen := binary.LittleEndian.Uint32(resp[:4])
	if len(resp) < int(4+strLen) {
		return "", fmt.Errorf("PINE: truncated title")
	}
	s := resp[4 : 4+strLen]
	if len(s) > 0 && s[len(s)-1] == 0 {
		s = s[:len(s)-1]
	}
	return string(s), nil
}

// GameID returns the game serial (e.g., "SLUS-20744").
func (p *PINEClient) GameID() (string, error) {
	resp, err := p.send([]byte{pineID})
	if err != nil {
		return "", err
	}
	if len(resp) < 4 {
		return "", fmt.Errorf("PINE: short ID response")
	}
	strLen := binary.LittleEndian.Uint32(resp[:4])
	if len(resp) < int(4+strLen) {
		return "", fmt.Errorf("PINE: truncated ID")
	}
	s := resp[4 : 4+strLen]
	if len(s) > 0 && s[len(s)-1] == 0 {
		s = s[:len(s)-1]
	}
	return string(s), nil
}

// PlayerPos reads the player position via PINE.
func (p *PINEClient) PlayerPos() (x, y, z float32, err error) {
	x, err = p.ReadF32(PlayerXAddr)
	if err != nil {
		return
	}
	y, err = p.ReadF32(PlayerYAddr)
	if err != nil {
		return
	}
	z, err = p.ReadF32(PlayerZAddr)
	return
}
