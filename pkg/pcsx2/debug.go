package pcsx2

import (
	"encoding/binary"
	"fmt"
)

// DebugState represents the hook's debug region at 0x000FA800.
type DebugState struct {
	RequestSector uint32 // +0x00
	RequestCount  uint32 // +0x04
	RequestDest   uint32 // +0x08
	RequestFlag   uint32 // +0x0C: 0=idle, 1=pending, 2=done, 3=error
	TotalCalls    uint32 // +0x10
	LastSector    uint32 // +0x14
	TunariaHits   uint32 // +0x18
	LastTunSector uint32 // +0x1C
	RedirectCount uint32 // +0x20
	LastXferSize  uint32 // +0x24
}

const debugStateSize = 0x28 // 10 × uint32

// ReadDebugState reads the full debug region.
func (p *PCSX2) ReadDebugState() (*DebugState, error) {
	return ReadDebugStateFrom(p)
}

func parseDebugState(buf []byte) *DebugState {
	return &DebugState{
		RequestSector: binary.LittleEndian.Uint32(buf[0x00:]),
		RequestCount:  binary.LittleEndian.Uint32(buf[0x04:]),
		RequestDest:   binary.LittleEndian.Uint32(buf[0x08:]),
		RequestFlag:   binary.LittleEndian.Uint32(buf[0x0C:]),
		TotalCalls:    binary.LittleEndian.Uint32(buf[0x10:]),
		LastSector:    binary.LittleEndian.Uint32(buf[0x14:]),
		TunariaHits:   binary.LittleEndian.Uint32(buf[0x18:]),
		LastTunSector: binary.LittleEndian.Uint32(buf[0x1C:]),
		RedirectCount: binary.LittleEndian.Uint32(buf[0x20:]),
		LastXferSize:  binary.LittleEndian.Uint32(buf[0x24:]),
	}
}

// FlagName returns a human-readable name for the request flag.
func FlagName(flag uint32) string {
	switch flag {
	case 0:
		return "idle"
	case 1:
		return "PENDING"
	case 2:
		return "done"
	case 3:
		return "error"
	default:
		return fmt.Sprintf("unknown(%d)", flag)
	}
}

// Format returns a human-readable debug state summary.
func (s *DebugState) Format() string {
	tunOffsetMB := float64(0)
	if s.LastTunSector >= TunariaStartSector {
		tunOffsetMB = float64(s.LastTunSector-TunariaStartSector) * float64(SectorSize) / (1024 * 1024)
	}
	xferKB := float64(s.LastXferSize) / 1024

	return fmt.Sprintf(`=== TUNARIA Host Redirect ===
Request flag:    %s
Total CdStream:  %d
Last sector:     %d
TUNARIA hits:    %d
Last TUN sector: %d (offset %.1f MB)
Redirects:       %d
Last xfer:       %d bytes (%.1f KB)`,
		FlagName(s.RequestFlag),
		s.TotalCalls,
		s.LastSector,
		s.TunariaHits,
		s.LastTunSector, tunOffsetMB,
		s.RedirectCount,
		s.LastXferSize, xferKB,
	)
}
