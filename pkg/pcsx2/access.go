package pcsx2

import (
	"fmt"
	"math"
)

// EEAccess is the interface for reading/writing PS2 EE RAM.
// Implemented by both PCSX2 (via /proc/pid/mem) and PINEClient (via IPC socket).
type EEAccess interface {
	Read(addr uint32, size int) ([]byte, error)
	Write(addr uint32, data []byte) error
	ReadU32(addr uint32) (uint32, error)
	WriteU32(addr uint32, val uint32) error
}

// ReadF32From reads a float32 via any EEAccess.
func ReadF32From(ee EEAccess, addr uint32) (float32, error) {
	v, err := ee.ReadU32(addr)
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(v), nil
}

// PlayerPosFrom reads player position via any EEAccess.
func PlayerPosFrom(ee EEAccess) (x, y, z float32, err error) {
	x, err = ReadF32From(ee, PlayerXAddr)
	if err != nil {
		return
	}
	y, err = ReadF32From(ee, PlayerYAddr)
	if err != nil {
		return
	}
	z, err = ReadF32From(ee, PlayerZAddr)
	return
}

// ReadDebugState reads the full debug region from any EEAccess implementation.
func ReadDebugStateFrom(ee EEAccess) (*DebugState, error) {
	buf, err := ee.Read(DebugBase, debugStateSize)
	if err != nil {
		return nil, err
	}
	return parseDebugState(buf), nil
}

// ValidateHookFrom checks if the PNACH code cave is installed via any EEAccess.
func ValidateHookFrom(ee EEAccess) error {
	v, err := ee.ReadU32(CodeCaveAddr)
	if err != nil {
		return err
	}
	if v != 0x3C0F000F {
		return fmt.Errorf("code cave not installed (got 0x%08X, expected 0x3C0F000F)", v)
	}
	return nil
}
