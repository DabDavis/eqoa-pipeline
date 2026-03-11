// eqoa-debug: Read-only debugging tools for EQOA on PCSX2.
//
// Reads PCSX2's EE RAM via PINE IPC to inspect player state,
// scan entities, and dump memory. No PNACH hook required —
// works with any running PCSX2 instance playing EQOA.
//
// Usage:
//
//	eqoa-debug player                        # show player info
//	eqoa-debug entities                      # list nearby NPCs/players
//	eqoa-debug read 0x01FBBA58               # read EE RAM address
//	eqoa-debug dump 0x01FBBA00 256           # hex dump
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/eqoa/iso-pipeline/pkg/pcsx2"
)

func main() {
	log.SetFlags(log.Ltime)

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "player":
		cmdPlayer(os.Args[2:])
	case "entities":
		cmdEntities(os.Args[2:])
	case "zone":
		cmdZone(os.Args[2:])
	case "pos":
		cmdPos(os.Args[2:])
	case "read":
		cmdRead(os.Args[2:])
	case "dump":
		cmdDump(os.Args[2:])
	case "info":
		cmdInfo(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `eqoa-debug — Live debugging tools for EQOA on PCSX2

Connects to PCSX2 via PINE IPC (no special setup needed — just enable
PINE in PCSX2 settings). Works with any EQOA session.

Commands:
  player     [--watch]              Player info (name, class, level, HP, position)
  entities                          List visible entities (NPCs, players)
  zone       [--watch]              Show current world/zone
  pos        [--watch]              Track player position
  read       <0xADDR>              Read value at EE RAM address
  dump       <0xADDR> <length>     Hex dump EE RAM region
  info                              PCSX2 connection info (version, game, status)

Options:
  --pid N     Use /proc/PID/mem instead of PINE (Linux, needs same-user PCSX2)
  --watch     Continuous monitoring (where supported)

Examples:
  eqoa-debug player                 Show character stats
  eqoa-debug entities               Who's nearby?
  eqoa-debug pos --watch            Live position tracker
  eqoa-debug dump 0x006F35D0 240    Dump first entity record
`)
}

// connect returns an EEAccess — PINE by default, /proc if --pid given.
func connect(pidFlag int) pcsx2.EEAccess {
	if pidFlag > 0 {
		p, err := pcsx2.FindWithPID(pidFlag)
		if err != nil {
			log.Fatalf("PCSX2 PID %d: %v", pidFlag, err)
		}
		log.Printf("PCSX2 PID: %d (/proc)", p.PID)
		return p
	}
	pine, err := pcsx2.PINEConnect()
	if err != nil {
		log.Fatal(err)
	}
	return pine
}

// --- info ---

func cmdInfo(args []string) {
	pine, err := pcsx2.PINEConnect()
	if err != nil {
		log.Fatal(err)
	}
	defer pine.Close()

	ver, err := pine.Version()
	if err != nil {
		fmt.Printf("Version: (error: %v)\n", err)
	} else {
		fmt.Printf("Version: %s\n", ver)
	}

	status, err := pine.Status()
	if err != nil {
		fmt.Printf("Status:  (error: %v)\n", err)
	} else {
		names := map[uint32]string{0: "Running", 1: "Paused", 2: "Shutdown"}
		name := names[status]
		if name == "" {
			name = fmt.Sprintf("Unknown(%d)", status)
		}
		fmt.Printf("Status:  %s\n", name)
	}

	if title, err := pine.GameTitle(); err == nil && title != "" {
		fmt.Printf("Game:    %s\n", title)
	}
	if id, err := pine.GameID(); err == nil && id != "" {
		fmt.Printf("ID:      %s\n", id)
	}
}

// --- player ---

func cmdPlayer(args []string) {
	fs := flag.NewFlagSet("player", flag.ExitOnError)
	watch := fs.Bool("watch", false, "Continuous monitoring")
	interval := fs.Float64("interval", 1.0, "Watch interval in seconds")
	pid := fs.Int("pid", 0, "Use /proc/PID/mem instead of PINE")
	fs.Parse(args)

	ee := connect(*pid)

	if !*watch {
		info, err := pcsx2.ReadPlayerInfo(ee)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(info.Format())
		return
	}

	var prevWorld int32 = -1
	for {
		info, err := pcsx2.ReadPlayerInfo(ee)
		if err != nil {
			time.Sleep(time.Duration(*interval * float64(time.Second)))
			continue
		}
		if info.World != prevWorld {
			fmt.Println(info.Format())
			fmt.Println()
			prevWorld = info.World
		}
		time.Sleep(time.Duration(*interval * float64(time.Second)))
	}
}

// --- entities ---

func cmdEntities(args []string) {
	fs := flag.NewFlagSet("entities", flag.ExitOnError)
	pid := fs.Int("pid", 0, "Use /proc/PID/mem instead of PINE")
	fs.Parse(args)

	ee := connect(*pid)

	entities, err := pcsx2.ScanEntities(ee)
	if err != nil {
		log.Fatal(err)
	}

	x, y, z, _ := pcsx2.PlayerPosFrom(ee)
	fmt.Printf("Player: %.0f, %.0f, %.0f\n", x, y, z)
	fmt.Println(pcsx2.FormatEntities(entities, x, z))
}

// --- zone ---

func cmdZone(args []string) {
	fs := flag.NewFlagSet("zone", flag.ExitOnError)
	watch := fs.Bool("watch", false, "Continuous monitoring")
	interval := fs.Float64("interval", 1.0, "Watch interval in seconds")
	pid := fs.Int("pid", 0, "Use /proc/PID/mem instead of PINE")
	fs.Parse(args)

	ee := connect(*pid)

	readLive := func() (int32, float32, float32, float32) {
		w, _ := ee.ReadU32(pcsx2.LiveWorldAddr)
		xv, _ := ee.ReadU32(pcsx2.LiveXAddr)
		yv, _ := ee.ReadU32(pcsx2.LiveYAddr)
		zv, _ := ee.ReadU32(pcsx2.LiveZAddr)
		return int32(w), math.Float32frombits(xv), math.Float32frombits(yv), math.Float32frombits(zv)
	}

	if !*watch {
		world, x, y, z := readLive()
		fmt.Printf("World: %s (%d)  Pos(%.0f, %.0f, %.0f)\n", pcsx2.WorldName(world), world, x, y, z)
		return
	}

	var prev int32 = -1
	for {
		world, x, y, z := readLive()
		if world != prev {
			fmt.Printf("World: %s (%d)  Pos(%.0f, %.0f, %.0f)\n", pcsx2.WorldName(world), world, x, y, z)
			prev = world
		}
		time.Sleep(time.Duration(*interval * float64(time.Second)))
	}
}

// --- pos ---

func cmdPos(args []string) {
	fs := flag.NewFlagSet("pos", flag.ExitOnError)
	watch := fs.Bool("watch", false, "Continuous tracking")
	interval := fs.Float64("interval", 1.0, "Watch interval in seconds")
	pid := fs.Int("pid", 0, "Use /proc/PID/mem instead of PINE")
	fs.Parse(args)

	ee := connect(*pid)

	if !*watch {
		x, y, z, err := pcsx2.PlayerPosFrom(ee)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Player: X=%.1f  Y=%.1f  Z=%.1f\n", x, y, z)
		return
	}

	var prevX, prevY, prevZ float32
	for {
		x, y, z, err := pcsx2.PlayerPosFrom(ee)
		if err != nil {
			time.Sleep(time.Duration(*interval * float64(time.Second)))
			continue
		}
		if x != prevX || y != prevY || z != prevZ {
			fmt.Printf("Player: X=%.1f  Y=%.1f  Z=%.1f\n", x, y, z)
			prevX, prevY, prevZ = x, y, z
		}
		time.Sleep(time.Duration(*interval * float64(time.Second)))
	}
}

// --- read ---

func cmdRead(args []string) {
	fs := flag.NewFlagSet("read", flag.ExitOnError)
	pid := fs.Int("pid", 0, "Use /proc/PID/mem instead of PINE")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: eqoa-debug read <0xADDRESS>\n")
		os.Exit(1)
	}

	ee := connect(*pid)
	addr := parseAddr(fs.Arg(0))

	u32, err := ee.ReadU32(addr)
	if err != nil {
		log.Fatal(err)
	}
	f32 := math.Float32frombits(u32)

	fmt.Printf("0x%08X:\n", addr)
	fmt.Printf("  uint32: %d (0x%08X)\n", u32, u32)
	fmt.Printf("  int32:  %d\n", int32(u32))
	fmt.Printf("  float:  %f\n", f32)
}

// --- dump ---

func cmdDump(args []string) {
	fs := flag.NewFlagSet("dump", flag.ExitOnError)
	pid := fs.Int("pid", 0, "Use /proc/PID/mem instead of PINE")
	fs.Parse(args)

	if fs.NArg() < 2 {
		fmt.Fprintf(os.Stderr, "Usage: eqoa-debug dump <0xADDRESS> <length>\n")
		os.Exit(1)
	}

	ee := connect(*pid)
	addr := parseAddr(fs.Arg(0))

	length, err := strconv.Atoi(fs.Arg(1))
	if err != nil {
		log.Fatalf("Invalid length %q: %v", fs.Arg(1), err)
	}
	if length > 4096 {
		length = 4096
	}

	data, err := ee.Read(addr, length)
	if err != nil {
		log.Fatal(err)
	}

	hexDump(data, addr)
}

// --- helpers ---

func parseAddr(s string) uint32 {
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		log.Fatalf("Invalid address %q: %v", s, err)
	}
	return uint32(v)
}

func hexDump(data []byte, baseAddr uint32) {
	_ = binary.LittleEndian // used implicitly
	for i := 0; i < len(data); i += 16 {
		fmt.Printf("%08X: ", baseAddr+uint32(i))
		for j := 0; j < 16; j++ {
			if i+j < len(data) {
				fmt.Printf("%02X ", data[i+j])
			} else {
				fmt.Printf("   ")
			}
			if j == 7 {
				fmt.Printf(" ")
			}
		}
		fmt.Printf(" |")
		for j := 0; j < 16 && i+j < len(data); j++ {
			b := data[i+j]
			if b >= 32 && b < 127 {
				fmt.Printf("%c", b)
			} else {
				fmt.Printf(".")
			}
		}
		fmt.Printf("|\n")
	}
}
