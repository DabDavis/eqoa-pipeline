package pcsx2

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

// Class/Race name tables.
var classNames = []string{
	"Warrior", "Ranger", "Paladin", "Shadow Knight", "Monk",
	"Bard", "Rogue", "Druid", "Shaman", "Cleric",
	"Magician", "Necromancer", "Enchanter", "Wizard", "Alchemist",
}

var raceNames = []string{
	"Human", "Elf", "Dark Elf", "Gnome", "Dwarf",
	"Troll", "Barbarian", "Halfling", "Erudite", "Ogre",
}

// PlayerInfo holds the full player data block.
type PlayerInfo struct {
	Name   string
	Class  int32
	Race   int32
	Level  int32
	World  int32
	X, Y, Z float32
	Facing float32
	HP     int32
	MaxHP  int32
	Power  int32
	MaxPow int32
}

// ClassName returns the class name string.
func (p *PlayerInfo) ClassName() string {
	if p.Class >= 0 && int(p.Class) < len(classNames) {
		return classNames[p.Class]
	}
	return fmt.Sprintf("Unknown(%d)", p.Class)
}

// RaceName returns the race name string.
func (p *PlayerInfo) RaceName() string {
	if p.Race >= 0 && int(p.Race) < len(raceNames) {
		return raceNames[p.Race]
	}
	return fmt.Sprintf("Unknown(%d)", p.Race)
}

// WorldName returns the world name string.
func WorldName(world int32) string {
	switch world {
	case 0:
		return "Tunaria"
	case 2:
		return "Odus"
	default:
		return fmt.Sprintf("World %d", world)
	}
}

// Format returns a human-readable player info summary.
func (p *PlayerInfo) Format() string {
	var b strings.Builder
	fmt.Fprintf(&b, "=== Player Info ===\n")
	fmt.Fprintf(&b, "Name:     %s\n", p.Name)
	fmt.Fprintf(&b, "Class:    %s (%d)\n", p.ClassName(), p.Class)
	fmt.Fprintf(&b, "Race:     %s (%d)\n", p.RaceName(), p.Race)
	fmt.Fprintf(&b, "Level:    %d\n", p.Level)
	fmt.Fprintf(&b, "World:    %s (%d)\n", WorldName(p.World), p.World)
	fmt.Fprintf(&b, "Position: X=%.1f  Y=%.1f  Z=%.1f\n", p.X, p.Y, p.Z)
	fmt.Fprintf(&b, "HP:       %d / %d\n", p.HP, p.MaxHP)
	fmt.Fprintf(&b, "Power:    %d / %d", p.Power, p.MaxPow)
	return b.String()
}

// ReadPlayerInfo reads the full player data block via any EEAccess.
func ReadPlayerInfo(ee EEAccess) (*PlayerInfo, error) {
	// Read name (24 bytes from 0x10 offset)
	nameBuf, err := ee.Read(PlayerNameAddr, 24)
	if err != nil {
		return nil, fmt.Errorf("reading name: %w", err)
	}
	if idx := bytes.IndexByte(nameBuf, 0); idx >= 0 {
		nameBuf = nameBuf[:idx]
	}

	// Read numeric fields
	info := &PlayerInfo{
		Name: string(nameBuf),
	}

	if v, err := ee.ReadU32(PlayerClassAddr); err == nil {
		info.Class = int32(v)
	}
	if v, err := ee.ReadU32(PlayerRaceAddr); err == nil {
		info.Race = int32(v)
	}
	if v, err := ee.ReadU32(PlayerLevelAddr); err == nil {
		info.Level = int32(v)
	}
	// Live world and position (writable addresses).
	if v, err := ee.ReadU32(LiveWorldAddr); err == nil {
		info.World = int32(v)
	}
	if v, err := ee.ReadU32(LiveXAddr); err == nil {
		info.X = math.Float32frombits(v)
	}
	if v, err := ee.ReadU32(LiveYAddr); err == nil {
		info.Y = math.Float32frombits(v)
	}
	if v, err := ee.ReadU32(LiveZAddr); err == nil {
		info.Z = math.Float32frombits(v)
	}
	if v, err := ee.ReadU32(PlayerHPAddr); err == nil {
		info.HP = int32(v)
	}
	if v, err := ee.ReadU32(PlayerMaxHPAddr); err == nil {
		info.MaxHP = int32(v)
	}
	if v, err := ee.ReadU32(PlayerPowAddr); err == nil {
		info.Power = int32(v)
	}
	if v, err := ee.ReadU32(PlayerMaxPowAddr); err == nil {
		info.MaxPow = int32(v)
	}

	return info, nil
}


// WritePlayerPos writes a new player position (teleport).
func WritePlayerPos(ee EEAccess, x, y, z float32) error {
	xb := make([]byte, 4)
	yb := make([]byte, 4)
	zb := make([]byte, 4)
	binary.LittleEndian.PutUint32(xb, math.Float32bits(x))
	binary.LittleEndian.PutUint32(yb, math.Float32bits(y))
	binary.LittleEndian.PutUint32(zb, math.Float32bits(z))

	if err := ee.Write(LiveXAddr, xb); err != nil {
		return fmt.Errorf("writing X: %w", err)
	}
	if err := ee.Write(LiveYAddr, yb); err != nil {
		return fmt.Errorf("writing Y: %w", err)
	}
	if err := ee.Write(LiveZAddr, zb); err != nil {
		return fmt.Errorf("writing Z: %w", err)
	}
	return nil
}

// WritePlayerWorld writes a new world ID (triggers world change + load screen).
func WritePlayerWorld(ee EEAccess, world int32) error {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, uint32(world))
	return ee.Write(LiveWorldAddr, buf)
}
