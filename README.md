# eqoa-iso-pipeline

Debug and serve pipeline for EQOA on PCSX2. Intercepts PS2 disc reads via a PNACH hook and serves ESF zone data (original or patched) from the host through EE RAM shared memory. Also provides live debug commands via PINE IPC for player inspection, entity scanning, and teleportation.

## How It Works

```
PCSX2 (PS2 client)
  │  VICdStreamRead hooked by PNACH
  │  writes sector request to EE RAM (0x000FA800)
  │  spins waiting for flag=2 (done)
  ▼
eqoa-iso-pipeline serve
  │  polls /proc/PID/mem for flag=1 (pending)
  │  reads from ISO (or zone patch overlay)
  │  writes data to EE RAM destination
  │  sets flag=2 (done)
  ▼
PS2 client continues with served data
```

The ISO is never modified — zone patches are overlays served in-memory.

## Commands

```bash
# Serve world ESF reads from ISO (TUNARIA + ODUS + PLANESKY)
eqoa-iso-pipeline serve game.iso

# Serve with zone overlay patches
eqoa-iso-pipeline serve game.iso --patches ~/Documents/eqoa/ESF-changes/patches

# Show hook debug state
eqoa-iso-pipeline status
eqoa-iso-pipeline status --watch

# Player info (name, class, race, level, HP, position)
eqoa-iso-pipeline player

# Current world/zone
eqoa-iso-pipeline zone

# Scan visible entities (NPCs, players)
eqoa-iso-pipeline entities

# Teleport player
eqoa-iso-pipeline teleport 25400 100 15800

# Read arbitrary EE RAM address
eqoa-iso-pipeline read 0x01FBBA58

# Hex dump EE RAM region
eqoa-iso-pipeline dump 0x01FBBA00 256
```

All debug commands use PINE IPC by default. The serve loop uses `/proc/PID/mem` for the speed required by the PS2 hook's spin-wait.

## Package Structure

```
cmd/eqoa-iso-pipeline/main.go    CLI entry point
pkg/pcsx2/
  pcsx2.go       PCSX2 process discovery, constants
  access.go      EE RAM access (PINE + /proc)
  pine.go        PINE IPC client
  serve.go       ESF serve loop, zone patch loading
  player.go      Player info (name, class, HP, position)
  entities.go    Entity table scanning
  debug.go       Debug region struct (0x000FA800)
```

## World ESF Sector Ranges

| World | Start Sector | End Sector | Size |
|-------|-------------|------------|------|
| TUNARIA | 520000 | 1006934 | ~951 MB |
| ODUS | 1006934 | 1100906 | ~183 MB |
| PLANESKY | 1100906 | 1110589 | ~19 MB |

All three are served by the PNACH hook (sector range 520000–1110589).

## EE RAM Debug Region (0x000FA800)

| Offset | Field | Description |
|--------|-------|-------------|
| +0x00 | request_sector | Sector number |
| +0x04 | request_count | Sector count (×2048 bytes) |
| +0x08 | request_dest | EE RAM destination address |
| +0x0C | request_flag | 0=idle, 1=pending, 2=done, 3=error |
| +0x10 | total_calls | Total VICdStreamRead calls |
| +0x14 | last_sector | Last sector read |
| +0x18 | world_hits | World sector hits |
| +0x1C | last_world_sector | Last world sector |
| +0x20 | redirect_count | Fulfilled requests |
| +0x24 | last_xfer_size | Bytes in last transfer |

## Key EE RAM Addresses

| Address | Type | Description |
|---------|------|-------------|
| 0x01FB5D60 | int32 | World ID (0=Tunaria, 2=Odus) |
| 0x01FB65B0 | float32 | Live player X position |
| 0x01FB65B4 | float32 | Live player Y position |
| 0x01FB65B8 | float32 | Live player Z position |
| 0x01FBBA00 | — | Player Data Block (name, class, race, level, HP) |
| 0x006F35D0 | — | Entity table (240-byte stride, 50 slots) |

## Dependencies

None — stdlib only.

## Related

- **PNACH hook**: `~/Documents/eqoa/cheats/EEEE1FCC-loadhook.pnach`
- **esfpatch**: `go-eqoa-pkg/cmd/esfpatch/` — create zone overlay patches
- **esfextract**: `go-eqoa-pkg/cmd/esfextract/` — inspect zones, export OBJ
