# eqoa-iso-pipeline

Host-side serve pipeline for EQOA TUNARIA.ESF data. Intercepts PS2 disc reads via PNACH hook and serves zone data (original or patched) from the host filesystem through PCSX2's EE RAM shared memory. The ISO is never modified — zone patches are overlays served in-memory.

## Architecture

```
PCSX2 (PS2 client)
  │  VICdStreamRead hooked by PNACH
  │  writes request to EE RAM debug region (0x000FA800)
  │  spins waiting for flag=2 (done)
  ▼
eqoa-iso-pipeline serve (this tool)
  │  polls /proc/PID/fd/N shared memory for flag=1 (pending)
  │  reads sector/count/dest from debug region
  │  checks if read overlaps a zone patch overlay
  │  reads data from patch or original ISO
  │  writes data to EE RAM at dest address
  │  sets flag=2 (done)
  ▼
PS2 client continues with served data
```

## Package Structure

```
cmd/eqoa-iso-pipeline/main.go    CLI entry point (serve, status, pos, read)
pkg/pcsx2/
  pcsx2.go                       PCSX2 process discovery, EE RAM read/write
  debug.go                       Debug state struct (0x000FA800 region)
  serve.go                       TUNARIA serve loop, zone patch loading
```

## Commands

```bash
# Serve TUNARIA reads from ISO (baseline, unmodified)
eqoa-iso-pipeline serve game.iso

# Serve with zone overlay patches
eqoa-iso-pipeline serve game.iso --patches ~/Documents/eqoa/ESF-changes/patches

# Show hook debug state (request counts, last sector, etc.)
eqoa-iso-pipeline status
eqoa-iso-pipeline status --watch

# Read player position
eqoa-iso-pipeline pos
eqoa-iso-pipeline pos --watch

# Read arbitrary EE RAM address
eqoa-iso-pipeline read 0x1FBBA58
```

## pkg/pcsx2 — Importable Library

The `pkg/pcsx2` package can be imported by other Go tools:

```go
import "github.com/eqoa/iso-pipeline/pkg/pcsx2"

p, _ := pcsx2.Find()                    // auto-discover PCSX2
x, y, z, _ := p.PlayerPos()             // read player position
state, _ := p.ReadDebugState()           // read hook counters
val, _ := p.ReadU32(0x004C8C28)          // read arbitrary address
p.Write(0x01C00000, data)                // write to EE RAM
p.Serve(pcsx2.ServeConfig{...})          // run serve loop
```

## Zone Patch Format

Patches are file pairs in a directory: `zone_N.json` + `zone_N.bin`.

**JSON metadata** (`zone_84.json`):
```json
{
  "zone": 84,
  "iso_byte_offset": 1566101520,
  "tun_byte_offset": 501141520,
  "size": 9514642
}
```

**Binary data** (`zone_84.bin`): Raw zone bytes (same size as original zone in ISO).

Patches are created by `esfpatch` in `go-eqoa-pkg/cmd/esfpatch/`.

## EE RAM Debug Region (0x000FA800)

Written by the PNACH hook, read/written by the serve loop:

| Offset | Field | Description |
|--------|-------|-------------|
| +0x00 | request_sector | Sector number for read request |
| +0x04 | request_count | Number of sectors (×2048 bytes) |
| +0x08 | request_dest | EE RAM destination address |
| +0x0C | request_flag | 0=idle, 1=pending, 2=done, 3=error |
| +0x10 | total_calls | Total VICdStreamRead calls |
| +0x14 | last_sector | Last sector read |
| +0x18 | tunaria_hits | TUNARIA sector hits |
| +0x1C | last_tun_sector | Last TUNARIA sector |
| +0x20 | redirect_count | Fulfilled requests |
| +0x24 | last_xfer_size | Bytes in last transfer |

## Key Constants

- TUNARIA sectors: 520000–1006934 (~951 MB on ISO)
- ISO byte offset: sector × 2048
- EE RAM: 32 MB (0x00000000–0x02000000)
- Player position: X=0x1FBBA58, Y=0x1FBBA5C, Z=0x1FBBA60
- PNACH hook code: 0x000FA400, debug region: 0x000FA800

## Dependencies

None — stdlib only. No external Go modules required.

## Related Tools

| Tool | Location | Purpose |
|------|----------|---------|
| esfpatch | `go-eqoa-pkg/cmd/esfpatch/` | Create zone patches (color, yscale, actor swap) |
| esfextract | `go-eqoa-pkg/cmd/esfextract/` | Inspect zones/actors, OBJ export |
| PNACH hook | `~/Documents/eqoa/cheats/EEEE1FCC-loadhook.pnach` | PS2-side VICdStreamRead intercept |
| pcsx2_debug.py | `re-tools/pcsx2_debug.py` | Original Python version (kept for legacy/debug) |
