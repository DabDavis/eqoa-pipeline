// Package pcsx2 provides access to PCSX2's EE RAM via /proc shared memory.
package pcsx2

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// EE RAM constants.
const (
	EERAMSize = 0x02000000 // 32 MB

	// Debug region written by PNACH hook.
	DebugBase = 0x000FA800

	// Player data block (0x01FBBA00).
	PlayerDataBase = 0x01FBBA00
	PlayerNameAddr = 0x01FBBA10 // null-terminated string
	PlayerClassAddr = 0x01FBBA28 // int32
	PlayerRaceAddr  = 0x01FBBA2C // int32
	PlayerLevelAddr = 0x01FBBA30 // int32
	PlayerZoneAddr  = 0x01FBBA54 // int32
	PlayerXAddr     = 0x01FBBA58 // float32
	PlayerYAddr     = 0x01FBBA5C // float32
	PlayerZAddr     = 0x01FBBA60 // float32
	PlayerFacing    = 0x01FBBA64 // float32
	PlayerHPAddr    = 0x01FBBA8C // int32
	PlayerMaxHPAddr = 0x01FBBA90 // int32
	PlayerPowAddr   = 0x01FBBA94 // int32
	PlayerMaxPowAddr = 0x01FBBA98 // int32

	// Live world/position (writable — game respects these).
	LiveWorldAddr = 0x01FB5D60 // int32: 0=Tunaria, 2=Odus
	LiveXAddr     = 0x01FB65B0 // float32: X
	LiveYAddr     = 0x01FB65B4 // float32: Y
	LiveZAddr     = 0x01FB65B8 // float32: Z
	GravityAddr   = 0x01FB6504

	// Entity table (state channel + game object data).
	// 240-byte records, ~50 slots (includes XOR delta snapshots).
	EntityTableBase = 0x006F35D0
	EntityStride    = 240
	EntityMaxCount  = 50

	// Code cave start (for validation).
	CodeCaveAddr = 0x000FA400
)

// PCSX2 provides read/write access to a running PCSX2 instance's EE RAM.
type PCSX2 struct {
	PID    int
	EEPath string // /proc/PID/fd/N
}

// Find discovers a running PCSX2 process and locates its EE RAM shared memory.
func Find() (*PCSX2, error) {
	pid, err := findPID()
	if err != nil {
		return nil, err
	}
	eePath, err := findEERAMPath(pid)
	if err != nil {
		return nil, fmt.Errorf("PCSX2 PID %d: %w", pid, err)
	}
	return &PCSX2{PID: pid, EEPath: eePath}, nil
}

// FindWithPID uses a specific PID instead of auto-detecting.
func FindWithPID(pid int) (*PCSX2, error) {
	eePath, err := findEERAMPath(pid)
	if err != nil {
		return nil, fmt.Errorf("PCSX2 PID %d: %w", pid, err)
	}
	return &PCSX2{PID: pid, EEPath: eePath}, nil
}

// Read reads bytes from EE RAM at the given address.
func (p *PCSX2) Read(addr uint32, size int) ([]byte, error) {
	f, err := os.Open(p.EEPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	buf := make([]byte, size)
	if _, err := f.ReadAt(buf, int64(addr)); err != nil {
		return nil, err
	}
	return buf, nil
}

// Write writes bytes to EE RAM at the given address.
func (p *PCSX2) Write(addr uint32, data []byte) error {
	f, err := os.OpenFile(p.EEPath, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteAt(data, int64(addr))
	return err
}

// ReadU32 reads a little-endian uint32 from EE RAM.
func (p *PCSX2) ReadU32(addr uint32) (uint32, error) {
	buf, err := p.Read(addr, 4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(buf), nil
}

// ReadI32 reads a little-endian int32 from EE RAM.
func (p *PCSX2) ReadI32(addr uint32) (int32, error) {
	v, err := p.ReadU32(addr)
	return int32(v), err
}

// ReadF32 reads a little-endian float32 from EE RAM.
func (p *PCSX2) ReadF32(addr uint32) (float32, error) {
	v, err := p.ReadU32(addr)
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(v), nil
}

// WriteU32 writes a little-endian uint32 to EE RAM.
func (p *PCSX2) WriteU32(addr, val uint32) error {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, val)
	return p.Write(addr, buf)
}

// ValidateHook checks if the PNACH code cave is installed.
func (p *PCSX2) ValidateHook() error {
	v, err := p.ReadU32(CodeCaveAddr)
	if err != nil {
		return fmt.Errorf("reading code cave: %w", err)
	}
	// lui $t7, 0x000F — first instruction of the hook
	if v != 0x3C0F000F {
		return fmt.Errorf("code cave not installed (got 0x%08X, expected 0x3C0F000F)", v)
	}
	return nil
}

// PlayerPos reads the current player position.
func (p *PCSX2) PlayerPos() (x, y, z float32, err error) {
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

// findPID scans /proc for PCSX2 processes and returns the one with the largest RSS.
func findPID() (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, fmt.Errorf("reading /proc: %w", err)
	}

	bestPID := 0
	bestRSS := int64(0)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		// Check if this is a PCSX2 process.
		isPCSX2 := false
		if comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid)); err == nil {
			if strings.Contains(strings.ToLower(string(comm)), "pcsx2") {
				isPCSX2 = true
			}
		}
		if !isPCSX2 {
			if cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil {
				if strings.Contains(strings.ToLower(string(cmdline)), "pcsx2") {
					isPCSX2 = true
				}
			}
		}
		if !isPCSX2 {
			continue
		}

		// Get RSS (pages) from statm — field index 1.
		if statm, err := os.ReadFile(fmt.Sprintf("/proc/%d/statm", pid)); err == nil {
			fields := strings.Fields(string(statm))
			if len(fields) >= 2 {
				if rss, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
					if rss > bestRSS {
						bestRSS = rss
						bestPID = pid
					}
				}
			}
		}
	}

	if bestPID == 0 {
		return 0, fmt.Errorf("no PCSX2 process found")
	}
	return bestPID, nil
}

// findEERAMPath locates the EE RAM shared memory fd for a given PCSX2 PID.
func findEERAMPath(pid int) (string, error) {
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", fdDir, err)
	}

	for _, entry := range entries {
		linkPath := filepath.Join(fdDir, entry.Name())
		target, err := os.Readlink(linkPath)
		if err != nil {
			continue
		}
		lower := strings.ToLower(target)
		if strings.Contains(lower, "pcsx2") && (strings.Contains(lower, "shm") || strings.Contains(lower, "dev/shm")) {
			return linkPath, nil
		}
	}

	return "", fmt.Errorf("EE RAM shared memory not found")
}
