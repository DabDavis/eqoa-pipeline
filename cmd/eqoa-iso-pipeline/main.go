// eqoa-iso-pipeline: Serves EQOA world ESF data to PCSX2 via EE RAM.
//
// Intercepts PS2 disc reads via a PNACH hook and serves zone data
// (original or patched) from the host. The ISO is never modified —
// zone patches are overlays served in-memory.
//
// Requires the PNACH loadhook installed in PCSX2 cheats.
// See README.md for setup instructions.
//
// Usage:
//
//	eqoa-iso-pipeline serve game.iso                    # serve unmodified
//	eqoa-iso-pipeline serve game.iso --patches dir/     # serve with overlays
//	eqoa-iso-pipeline status                            # show hook state
//	eqoa-iso-pipeline status --watch                    # monitor hook
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
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
	case "serve":
		cmdServe(os.Args[2:])
	case "status":
		cmdStatus(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `eqoa-iso-pipeline — Serve EQOA world ESF data to PCSX2

Intercepts PS2 disc reads via a PNACH hook and serves zone data from
the host filesystem. Supports TUNARIA, ODUS, and PLANESKY world ESFs.
The ISO is never modified — zone patches are overlays served in-memory.

Requires the PNACH loadhook installed in PCSX2 cheats directory.

Commands:
  serve <game.iso> [flags]    Serve world ESF reads from ISO
  status [flags]              Show hook debug state

Serve flags:
  --patches <dir>    Directory of zone overlay patches (zone_N.json + zone_N.bin)
  --pid <N>          Override PCSX2 PID (default: auto-detect)
  --pine             Use PINE IPC instead of /proc/pid/mem

Status flags:
  --watch            Continuous monitoring
  --interval <sec>   Watch interval (default: 1.0)
  --pid <N>          Override PCSX2 PID
  --pine             Use PINE IPC

Examples:
  eqoa-iso-pipeline serve ~/eqoa.iso
  eqoa-iso-pipeline serve ~/eqoa.iso --patches ~/patches/
  eqoa-iso-pipeline status --watch
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
	return connectProc(pidFlag)
}

func connectProc(pidFlag int) *pcsx2.PCSX2 {
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
		fmt.Fprintf(os.Stderr, "Usage: eqoa-iso-pipeline serve <game.iso> [--patches dir]\n")
		os.Exit(1)
	}

	ee := connectAny(*pid, *usePine)

	if err := pcsx2.ValidateHookFrom(ee); err != nil {
		log.Printf("WARNING: %v", err)
	} else {
		log.Printf("Validated: code cave found (loadhook active)")
	}

	err := pcsx2.ServeWith(ee, pcsx2.ServeConfig{
		ISOPath:    positional[0],
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
