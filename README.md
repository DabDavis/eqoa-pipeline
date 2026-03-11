# eqoa-iso-pipeline

Tools for working with EQOA on PCSX2 — a **debug tool** for inspecting live game state, and a **serve pipeline** for modding zone data without touching the ISO.

## Tools

### `eqoa-debug` — Live Debugging

Connects to PCSX2 via [PINE IPC](https://wiki.pcsx2.net/PINE) to inspect and modify game state in real time. No special setup beyond enabling PINE in PCSX2 settings.

```bash
eqoa-debug player                    # character info (name, class, level, HP)
eqoa-debug entities                  # list nearby NPCs and players
eqoa-debug teleport 25400 100 15800  # move your character
eqoa-debug pos --watch               # live position tracker
eqoa-debug zone                      # current world (Tunaria/Odus)
eqoa-debug read 0x01FBBA58           # read any EE RAM address
eqoa-debug dump 0x006F35D0 240       # hex dump memory region
eqoa-debug info                      # PCSX2 version and game info
```

### `eqoa-iso-pipeline` — Zone Serve Pipeline

Intercepts PS2 disc reads via a PNACH hook and serves world ESF data (TUNARIA, ODUS, PLANESKY) from the host filesystem. Supports zone overlay patches — the ISO is never modified.

```bash
eqoa-iso-pipeline serve ~/eqoa.iso
eqoa-iso-pipeline serve ~/eqoa.iso --patches ~/my-patches/
eqoa-iso-pipeline status --watch
```

Requires the PNACH loadhook installed in your PCSX2 cheats directory. See [Setup](#pipeline-setup) below.

## Install

```bash
go install github.com/eqoa/iso-pipeline/cmd/eqoa-debug@latest
go install github.com/eqoa/iso-pipeline/cmd/eqoa-iso-pipeline@latest
```

Or build from source:

```bash
git clone <this-repo>
cd eqoa-iso-pipeline
go build -o eqoa-debug ./cmd/eqoa-debug
go build -o eqoa-iso-pipeline ./cmd/eqoa-iso-pipeline
```

## Debug Setup

1. Open PCSX2 settings
2. Enable **PINE** (Settings > Advanced > Enable PINE)
3. Run EQOA
4. Use `eqoa-debug` commands

That's it. PINE uses a local socket — no network, no risk.

## Pipeline Setup

The serve pipeline requires a PNACH hook that redirects disc reads through EE RAM shared memory.

1. Copy the PNACH loadhook to your PCSX2 cheats directory:
   ```
   cp EEEE1FCC-loadhook.pnach ~/.config/PCSX2/cheats/
   ```
2. Enable cheats in PCSX2 (System > Enable Cheats)
3. Start EQOA — the game will freeze at the loading screen waiting for the pipeline
4. Run: `eqoa-iso-pipeline serve /path/to/eqoa.iso`
5. The game continues loading with data served from the host

### Zone Patches

Create zone overlay patches with [esfpatch](https://github.com/eqoa/go-eqoa-pkg/cmd/esfpatch):

```bash
esfpatch -zone 84 -color ff0000 eqoa.iso    # red Freeport
```

This produces `zone_84.json` + `zone_84.bin`. Pass the directory to the pipeline:

```bash
eqoa-iso-pipeline serve eqoa.iso --patches ./my-patches/
```

## Package Structure

```
cmd/
  eqoa-debug/          Debug tool (PINE-based, no hook needed)
  eqoa-iso-pipeline/   Serve pipeline (requires PNACH hook)

pkg/pcsx2/
  pcsx2.go             PCSX2 process discovery, EE RAM constants
  pine.go              PINE IPC client
  access.go            EEAccess interface (shared by PINE + /proc)
  player.go            Player info reader/writer
  entities.go          Entity table scanner
  serve.go             ESF serve loop with zone patch overlay
  debug.go             Hook debug region parser
```

## EE RAM Reference

### Player

| Address | Type | Field |
|---------|------|-------|
| `0x01FBBA10` | string | Character name |
| `0x01FBBA28` | int32 | Class |
| `0x01FBBA2C` | int32 | Race |
| `0x01FBBA30` | int32 | Level |
| `0x01FB5D60` | int32 | World ID (0=Tunaria, 2=Odus) |
| `0x01FB65B0` | float32 | X position (writable) |
| `0x01FB65B4` | float32 | Y position (writable) |
| `0x01FB65B8` | float32 | Z position (writable) |
| `0x01FBBA8C` | int32 | HP |
| `0x01FBBA90` | int32 | Max HP |

### Entity Table

Base address: `0x006F35D0`, stride: 240 bytes, up to 50 slots.

| Offset | Type | Field |
|--------|------|-------|
| `+0x5C` | string(24) | Entity name |
| `+0x74` | byte | Level |
| `+0xC0` | ptr | Game object pointer (valid = has position) |
| `+0xD0` | float32 | X position |
| `+0xD4` | float32 | Y position |
| `+0xD8` | float32 | Z position |
| `+0xDC` | uint32 | Model DictID |

### World ESF Sectors

| World | Start | End | Size |
|-------|-------|-----|------|
| TUNARIA | 520000 | 1006934 | ~951 MB |
| ODUS | 1006934 | 1100906 | ~183 MB |
| PLANESKY | 1100906 | 1110589 | ~19 MB |

## Dependencies

None — Go stdlib only.

## Platform

Linux only. Uses `/proc` for process discovery and EE RAM access. PINE IPC works on any platform where PCSX2 supports it, but process auto-detection is Linux-specific.
