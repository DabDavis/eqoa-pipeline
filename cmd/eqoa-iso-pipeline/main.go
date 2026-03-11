// eqoa-iso-pipeline: EQOA TUNARIA.ESF host-side serve pipeline.
//
// Intercepts PS2 disc reads via PNACH hook and serves zone data from the
// host filesystem. The ISO is never modified — zone patches are overlays
// served in-memory.
//
// Usage:
//
//	eqoa-iso-pipeline serve game.iso                    # serve unmodified
//	eqoa-iso-pipeline serve game.iso --patches dir/     # serve with overlays
//	eqoa-iso-pipeline status                            # show hook debug state
//	eqoa-iso-pipeline pos                               # read player position
//	eqoa-iso-pipeline pos --watch                       # track position
//	eqoa-iso-pipeline read 0x1FBBA58                    # read arbitrary address
package main

import (
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

	cmd := os.Args[1]

	switch cmd {
	case "serve":
		cmdServe(os.Args[2:])
	case "status":
		cmdStatus(os.Args[2:])
	case "pos":
		cmdPos(os.Args[2:])
	case "read":
		cmdRead(os.Args[2:])
	case "dump":
		cmdDump(os.Args[2:])
	case "pine":
		cmdPine(os.Args[2:])
	case "player":
		cmdPlayer(os.Args[2:])
	case "entities":
		cmdEntities(os.Args[2:])
	case "teleport", "tp":
		cmdTeleport(os.Args[2:])
	case "zone":
		cmdZone(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `eqoa-iso-pipeline — EQOA TUNARIA host-side serve pipeline

Commands:
  serve <iso>  [--patches dir] [--pid N]   Serve TUNARIA reads from ISO with optional overlays
  status       [--watch] [--pid N]         Show hook debug state
  pos          [--watch] [--pid N]         Read player position
  player       [--watch] [--pine]          Show full player info (name, class, level, HP, zone)
  zone         [--watch] [--pine]          Show current zone ID
  entities     [--pine]                    Scan visible entity table
  teleport     <x> <y> <z> [--pine]       Teleport player to coordinates
  read <addr>  [--pid N]                   Read arbitrary EE RAM address (hex)
  dump <addr> <len> [--pid N]              Hex dump EE RAM region
  pine [pos|read <addr>|dump <addr> <len>] Connect via PINE IPC (no PID needed)

The ISO is never modified. Zone patches are overlays served in-memory.
`)
}

func connectAny(pidFlag int, usePine bool) pcsx2.EEAccess {
	if usePine {
		pine, err := pcsx2.PINEConnect()
		if err != nil {
			log.Fatalf("PINE: %v", err)
		}
		ver, _ := pine.Version()
		log.Printf("PINE: connected (%s)", ver)
		return pine
	}
	return connect(pidFlag)
}

func connect(pidFlag int) *pcsx2.PCSX2 {
	var p *pcsx2.PCSX2
	var err error
	if pidFlag > 0 {
		p, err = pcsx2.FindWithPID(pidFlag)
	} else {
		p, err = pcsx2.Find()
	}
	if err != nil {
		log.Fatalf("PCSX2: %v", err)
	}
	log.Printf("PCSX2 PID: %d", p.PID)
	log.Printf("EE RAM: %s", p.EEPath)
	return p
}

// --- serve ---

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	patchesDir := fs.String("patches", "", "Directory of zone overlay patches")
	pid := fs.Int("pid", 0, "Override PCSX2 PID")
	usePine := fs.Bool("pine", false, "Use PINE IPC instead of /proc/pid/mem")

	// Go's flag package stops at the first non-flag arg.
	// Separate flags from positional args manually.
	var positional []string
	var flagArgs []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			flagArgs = append(flagArgs, args[i])
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flagArgs = append(flagArgs, args[i+1])
				i++
			}
		} else {
			positional = append(positional, args[i])
		}
	}
	fs.Parse(flagArgs)

	if len(positional) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: eqoa-iso-pipeline serve <game.iso> [--patches dir] [--pine]\n")
		os.Exit(1)
	}
	isoPath := positional[0]

	var ee pcsx2.EEAccess
	if *usePine {
		pine, err := pcsx2.PINEConnect()
		if err != nil {
			log.Fatalf("PINE: %v", err)
		}
		ver, _ := pine.Version()
		log.Printf("PINE: connected (%s)", ver)
		ee = pine
	} else {
		p := connect(*pid)
		ee = p
	}

	if err := pcsx2.ValidateHookFrom(ee); err != nil {
		log.Printf("WARNING: %v", err)
	} else {
		log.Printf("Validated: code cave found (loadhook active)")
	}

	err := pcsx2.ServeWith(ee, pcsx2.ServeConfig{
		ISOPath:    isoPath,
		PatchesDir: *patchesDir,
	})
	if err != nil {
		log.Fatal(err)
	}
}

// --- status ---

func cmdStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	watch := fs.Bool("watch", false, "Continuous monitoring")
	interval := fs.Float64("interval", 1.0, "Watch interval in seconds")
	pid := fs.Int("pid", 0, "Override PCSX2 PID")
	usePine := fs.Bool("pine", false, "Use PINE IPC")
	fs.Parse(args)

	ee := connectAny(*pid, *usePine)

	if err := pcsx2.ValidateHookFrom(ee); err != nil {
		log.Printf("WARNING: %v", err)
	}

	if !*watch {
		state, err := pcsx2.ReadDebugStateFrom(ee)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(state.Format())
		return
	}

	var prev pcsx2.DebugState
	for {
		state, err := pcsx2.ReadDebugStateFrom(ee)
		if err != nil {
			time.Sleep(time.Duration(*interval * float64(time.Second)))
			continue
		}
		if *state != prev {
			fmt.Println(state.Format())
			fmt.Println()
			prev = *state
		}
		time.Sleep(time.Duration(*interval * float64(time.Second)))
	}
}

// --- pos ---

func cmdPos(args []string) {
	fs := flag.NewFlagSet("pos", flag.ExitOnError)
	watch := fs.Bool("watch", false, "Continuous tracking")
	interval := fs.Float64("interval", 1.0, "Watch interval in seconds")
	pid := fs.Int("pid", 0, "Override PCSX2 PID")
	usePine := fs.Bool("pine", false, "Use PINE IPC")
	fs.Parse(args)

	ee := connectAny(*pid, *usePine)

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
	pid := fs.Int("pid", 0, "Override PCSX2 PID")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: eqoa-iso-pipeline read <0xADDRESS>\n")
		os.Exit(1)
	}

	addrStr := strings.TrimPrefix(fs.Arg(0), "0x")
	addrStr = strings.TrimPrefix(addrStr, "0X")
	addr64, err := strconv.ParseUint(addrStr, 16, 32)
	if err != nil {
		log.Fatalf("Invalid address %q: %v", fs.Arg(0), err)
	}
	addr := uint32(addr64)

	p := connect(*pid)

	u32, err := p.ReadU32(addr)
	if err != nil {
		log.Fatal(err)
	}
	f32, _ := p.ReadF32(addr)
	i32, _ := p.ReadI32(addr)

	fmt.Printf("Address 0x%08X:\n", addr)
	fmt.Printf("  uint32: %d (0x%08X)\n", u32, u32)
	fmt.Printf("  int32:  %d\n", i32)
	fmt.Printf("  float:  %f\n", f32)
}

// --- dump ---

func cmdDump(args []string) {
	fs := flag.NewFlagSet("dump", flag.ExitOnError)
	pid := fs.Int("pid", 0, "Override PCSX2 PID")
	fs.Parse(args)

	if fs.NArg() < 2 {
		fmt.Fprintf(os.Stderr, "Usage: eqoa-iso-pipeline dump <0xADDRESS> <length>\n")
		os.Exit(1)
	}

	addrStr := strings.TrimPrefix(fs.Arg(0), "0x")
	addrStr = strings.TrimPrefix(addrStr, "0X")
	addr64, err := strconv.ParseUint(addrStr, 16, 32)
	if err != nil {
		log.Fatalf("Invalid address %q: %v", fs.Arg(0), err)
	}
	addr := uint32(addr64)

	length, err := strconv.Atoi(fs.Arg(1))
	if err != nil {
		log.Fatalf("Invalid length %q: %v", fs.Arg(1), err)
	}
	if length > 4096 {
		length = 4096
	}

	p := connect(*pid)

	data, err := p.Read(addr, length)
	if err != nil {
		log.Fatal(err)
	}

	// Hex dump with ASCII
	for i := 0; i < len(data); i += 16 {
		fmt.Printf("%08X: ", addr+uint32(i))
		// Hex bytes
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
		// ASCII
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

// --- pine ---

func cmdPine(args []string) {
	pine, err := pcsx2.PINEConnect()
	if err != nil {
		log.Fatal(err)
	}
	defer pine.Close()

	ver, err := pine.Version()
	if err != nil {
		log.Printf("WARNING: version query failed: %v", err)
	} else {
		fmt.Printf("Connected: %s\n", ver)
	}

	status, err := pine.Status()
	if err != nil {
		log.Printf("WARNING: status query failed: %v", err)
	} else {
		statusStr := "Unknown"
		switch status {
		case 0:
			statusStr = "Running"
		case 1:
			statusStr = "Paused"
		case 2:
			statusStr = "Shutdown"
		}
		fmt.Printf("Status: %s\n", statusStr)
	}

	title, _ := pine.GameTitle()
	if title != "" {
		fmt.Printf("Game: %s\n", title)
	}
	gameID, _ := pine.GameID()
	if gameID != "" {
		fmt.Printf("ID: %s\n", gameID)
	}

	// Sub-commands
	if len(args) == 0 {
		// Default: show info + player pos
		x, y, z, err := pine.PlayerPos()
		if err != nil {
			log.Printf("Player pos: %v", err)
		} else {
			fmt.Printf("Player: X=%.1f  Y=%.1f  Z=%.1f\n", x, y, z)
		}
		return
	}

	switch args[0] {
	case "pos":
		x, y, z, err := pine.PlayerPos()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Player: X=%.1f  Y=%.1f  Z=%.1f\n", x, y, z)

	case "read":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: pine read <0xADDRESS>\n")
			os.Exit(1)
		}
		addrStr := strings.TrimPrefix(args[1], "0x")
		addrStr = strings.TrimPrefix(addrStr, "0X")
		addr64, err := strconv.ParseUint(addrStr, 16, 32)
		if err != nil {
			log.Fatalf("Invalid address: %v", err)
		}
		addr := uint32(addr64)
		u32, err := pine.ReadU32(addr)
		if err != nil {
			log.Fatal(err)
		}
		f32, _ := pine.ReadF32(addr)
		fmt.Printf("Address 0x%08X:\n", addr)
		fmt.Printf("  uint32: %d (0x%08X)\n", u32, u32)
		fmt.Printf("  int32:  %d\n", int32(u32))
		fmt.Printf("  float:  %f\n", f32)

	case "dump":
		if len(args) < 3 {
			fmt.Fprintf(os.Stderr, "Usage: pine dump <0xADDRESS> <length>\n")
			os.Exit(1)
		}
		addrStr := strings.TrimPrefix(args[1], "0x")
		addrStr = strings.TrimPrefix(addrStr, "0X")
		addr64, err := strconv.ParseUint(addrStr, 16, 32)
		if err != nil {
			log.Fatalf("Invalid address: %v", err)
		}
		addr := uint32(addr64)
		length, err := strconv.Atoi(args[2])
		if err != nil {
			log.Fatalf("Invalid length: %v", err)
		}
		if length > 4096 {
			length = 4096
		}

		data, err := pine.Read(addr, length)
		if err != nil {
			log.Fatal(err)
		}
		for i := 0; i < len(data); i += 16 {
			fmt.Printf("%08X: ", addr+uint32(i))
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

	default:
		fmt.Fprintf(os.Stderr, "Unknown pine sub-command: %s\n", args[0])
		fmt.Fprintf(os.Stderr, "Usage: pine [pos|read <addr>|dump <addr> <len>]\n")
		os.Exit(1)
	}
}

// --- player ---

func cmdPlayer(args []string) {
	fs := flag.NewFlagSet("player", flag.ExitOnError)
	watch := fs.Bool("watch", false, "Continuous monitoring")
	interval := fs.Float64("interval", 1.0, "Watch interval in seconds")
	pid := fs.Int("pid", 0, "Override PCSX2 PID")
	usePine := fs.Bool("pine", false, "Use PINE IPC")
	fs.Parse(args)

	ee := connectAny(*pid, *usePine)

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

// --- zone ---

func cmdZone(args []string) {
	fs := flag.NewFlagSet("zone", flag.ExitOnError)
	watch := fs.Bool("watch", false, "Continuous monitoring")
	interval := fs.Float64("interval", 1.0, "Watch interval in seconds")
	pid := fs.Int("pid", 0, "Override PCSX2 PID")
	usePine := fs.Bool("pine", false, "Use PINE IPC")
	fs.Parse(args)

	ee := connectAny(*pid, *usePine)

	readLive := func() (int32, float32, float32, float32) {
		w, _ := ee.ReadU32(pcsx2.LiveWorldAddr)
		xv, _ := ee.ReadU32(pcsx2.LiveXAddr)
		yv, _ := ee.ReadU32(pcsx2.LiveYAddr)
		zv, _ := ee.ReadU32(pcsx2.LiveZAddr)
		x := math.Float32frombits(xv)
		y := math.Float32frombits(yv)
		z := math.Float32frombits(zv)
		return int32(w), x, y, z
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

// --- entities ---

func cmdEntities(args []string) {
	fs := flag.NewFlagSet("entities", flag.ExitOnError)
	pid := fs.Int("pid", 0, "Override PCSX2 PID")
	usePine := fs.Bool("pine", false, "Use PINE IPC")
	fs.Parse(args)

	ee := connectAny(*pid, *usePine)

	entities, err := pcsx2.ScanEntities(ee)
	if err != nil {
		log.Fatal(err)
	}

	x, y, z, _ := pcsx2.PlayerPosFrom(ee)
	_ = y
	fmt.Printf("Player: %.0f, %.0f, %.0f\n", x, y, z)
	fmt.Println(pcsx2.FormatEntities(entities, x, z))
}

// --- teleport ---

func cmdTeleport(args []string) {
	fs := flag.NewFlagSet("teleport", flag.ExitOnError)
	pid := fs.Int("pid", 0, "Override PCSX2 PID")
	usePine := fs.Bool("pine", false, "Use PINE IPC")
	world := fs.Int("world", -1, "World ID (0=Tunaria, 2=Odus)")
	fs.Parse(args)

	if fs.NArg() < 3 && *world < 0 {
		fmt.Fprintf(os.Stderr, "Usage: eqoa-iso-pipeline teleport <x> <y> <z> [--world N] [--pine]\n")
		fmt.Fprintf(os.Stderr, "       eqoa-iso-pipeline teleport --world 0 [--pine]   # change world only\n")
		os.Exit(1)
	}

	ee := connectAny(*pid, *usePine)

	if fs.NArg() >= 3 {
		x, err := strconv.ParseFloat(fs.Arg(0), 32)
		if err != nil {
			log.Fatalf("Invalid X: %v", err)
		}
		y, err := strconv.ParseFloat(fs.Arg(1), 32)
		if err != nil {
			log.Fatalf("Invalid Y: %v", err)
		}
		z, err := strconv.ParseFloat(fs.Arg(2), 32)
		if err != nil {
			log.Fatalf("Invalid Z: %v", err)
		}

		if err := pcsx2.WritePlayerPos(ee, float32(x), float32(y), float32(z)); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Position: X=%.1f  Y=%.1f  Z=%.1f\n", x, y, z)
	}

	if *world >= 0 {
		if err := pcsx2.WritePlayerWorld(ee, int32(*world)); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("World: %s (%d)\n", pcsx2.WorldName(int32(*world)), *world)
	}
}
