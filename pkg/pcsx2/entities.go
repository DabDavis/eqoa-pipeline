package pcsx2

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

// EntityInfo holds info about a visible entity.
type EntityInfo struct {
	Index   int
	Addr    uint32
	Name    string
	X, Y, Z float32
	ModelID uint32
	Level   byte
	Race    byte
	Class   byte
}

// Entity table layout.
// The state channel buffer at EntityTableBase stores 240-byte entity records.
// Each record contains network state + runtime game object data.
const (
	entityStride     = 240
	entityScanCount  = 50 // scan up to 50 slots (includes XOR delta snapshots)

	entNameOff     = 0x5C // 24-byte null-padded ASCII name
	entLevelOff    = 0x74
	entRaceOff     = 0x78 // race byte
	entClassOff    = 0x79 // class byte
	entPtrOff      = 0xC0 // EE RAM pointer — if > 0x00100000, entity has live position
	entPosXOff     = 0xD0 // float32 X
	entPosYOff     = 0xD4 // float32 Y
	entPosZOff     = 0xD8 // float32 Z
	entModelIDOff  = 0xDC // uint32 model ID (at position block)
)

// ScanEntities reads the entity table and returns active entities with valid positions.
func ScanEntities(ee EEAccess) ([]EntityInfo, error) {
	var entities []EntityInfo
	seen := make(map[string]bool) // deduplicate by name

	for i := 0; i < entityScanCount; i++ {
		base := uint32(EntityTableBase) + uint32(i)*uint32(entityStride)
		if base+entityStride > EERAMSize {
			break
		}

		buf, err := ee.Read(base, entityStride)
		if err != nil {
			continue
		}

		// Check header: active entities start with 00 00 01 FF
		hdr := binary.LittleEndian.Uint32(buf[:4])
		if hdr != 0xFF010000 {
			continue
		}

		// Check if entity has a live game object (pointer at 0xC0 in EE RAM range)
		ptr := binary.LittleEndian.Uint32(buf[entPtrOff:])
		if ptr < 0x00100000 || ptr >= EERAMSize {
			continue
		}

		// Extract name
		nameBytes := buf[entNameOff : entNameOff+24]
		if idx := indexOf(nameBytes, 0); idx >= 0 {
			nameBytes = nameBytes[:idx]
		}
		name := string(nameBytes)
		if len(name) == 0 || !isPrintable(nameBytes[0]) {
			continue
		}

		// Deduplicate (XOR delta creates multiple snapshots)
		if seen[name] {
			continue
		}
		seen[name] = true

		// Read float position
		x := math.Float32frombits(binary.LittleEndian.Uint32(buf[entPosXOff:]))
		y := math.Float32frombits(binary.LittleEndian.Uint32(buf[entPosYOff:]))
		z := math.Float32frombits(binary.LittleEndian.Uint32(buf[entPosZOff:]))

		modelID := binary.LittleEndian.Uint32(buf[entModelIDOff:])

		ent := EntityInfo{
			Index:   i,
			Addr:    base,
			Name:    name,
			X:       x,
			Y:       y,
			Z:       z,
			ModelID: modelID,
			Level:   buf[entLevelOff],
			Race:    buf[entRaceOff],
			Class:   buf[entClassOff],
		}

		entities = append(entities, ent)
	}

	return entities, nil
}

// FormatEntities returns a formatted entity list with distances from player.
func FormatEntities(entities []EntityInfo, playerX, playerZ float32) string {
	if len(entities) == 0 {
		return "No active entities"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "=== Entities (%d active) ===\n", len(entities))
	for _, e := range entities {
		dx := e.X - playerX
		dz := e.Z - playerZ
		dist := math.Sqrt(float64(dx*dx + dz*dz))

		raceName := ""
		if int(e.Race) < len(raceNames) {
			raceName = raceNames[e.Race]
		}
		className := ""
		if int(e.Class) < len(classNames) {
			className = classNames[e.Class]
		}

		fmt.Fprintf(&b, "  %-20s Lv%-3d %-10s %-14s Pos(%.0f, %.0f, %.0f)  Dist=%.0f  Model=0x%08X\n",
			e.Name, e.Level, raceName, className,
			e.X, e.Y, e.Z, dist, e.ModelID)
	}
	return b.String()
}

func indexOf(buf []byte, val byte) int {
	for i, b := range buf {
		if b == val {
			return i
		}
	}
	return -1
}

func isPrintable(b byte) bool {
	return b >= 32 && b < 127
}
