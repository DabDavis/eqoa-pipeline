package pcsx2

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// World ESF disc layout constants.
const (
	SectorSize         = 2048
	TunariaStartSector = 520000
	TunariaEndSector   = 1006934
	TunariaByteOffset  = TunariaStartSector * SectorSize // 0x3F7A0000
	TunariaByteSize    = (TunariaEndSector - TunariaStartSector) * SectorSize

	// All world ESFs are contiguous: TUNARIA → ODUS → PLANESKY
	OdusStartSector     = 1006934
	OdusEndSector       = 1100906
	PlaneSkyStartSector = 1100906
	PlaneSkyEndSector   = 1110589

	// Combined range for hook: 520000 - 1110589
	WorldStartSector = TunariaStartSector // 520000
	WorldEndSector   = PlaneSkyEndSector  // 1110589

	// Request flag offsets relative to DebugBase.
	offRequestSector = 0x00
	offRequestCount  = 0x04
	offRequestDest   = 0x08
	offRequestFlag   = 0x0C
	offLastXferSize  = 0x24

	flagIdle    = 0
	flagPending = 1
	flagDone    = 2
	flagError   = 3
)

// ZonePatch holds a loaded zone overlay patch.
type ZonePatch struct {
	Zone           int
	ISOByteOffset  int64
	TunByteOffset  int64
	Size           int
	Data           []byte
}

// ServeConfig configures the TUNARIA serve loop.
type ServeConfig struct {
	// ISOPath is the path to the game ISO (or standalone TUNARIA.ESF).
	ISOPath string

	// PatchesDir is an optional directory of zone_N.bin/json overlays.
	PatchesDir string

	// Logger for output. If nil, uses log.Default().
	Logger *log.Logger
}

// Serve runs the main TUNARIA serve loop. It blocks until interrupted.
func (p *PCSX2) Serve(cfg ServeConfig) error {
	return ServeWith(p, cfg)
}

// ServeWith runs the serve loop using any EEAccess implementation (PCSX2 or PINE).
func ServeWith(p EEAccess, cfg ServeConfig) error {
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}

	isISO := isISOFile(cfg.ISOPath)

	// Load zone patches.
	var patches []ZonePatch
	if cfg.PatchesDir != "" {
		var err error
		patches, err = LoadZonePatches(cfg.PatchesDir, isISO)
		if err != nil {
			logger.Printf("WARNING: loading patches: %v", err)
		}
		if len(patches) > 0 {
			for _, patch := range patches {
				logger.Printf("  Loaded zone %d patch: offset=0x%x size=%d (%.1f MB)",
					patch.Zone, patchOffset(patch, isISO), patch.Size, float64(patch.Size)/(1024*1024))
			}
			logger.Printf("  %d zone patch(es) active", len(patches))
		} else {
			logger.Printf("  No patches found in %s", cfg.PatchesDir)
		}
	}

	// Open source file.
	logger.Printf("Opening %s...", cfg.ISOPath)
	srcFile, err := os.Open(cfg.ISOPath)
	if err != nil {
		return fmt.Errorf("opening %s: %w", cfg.ISOPath, err)
	}
	defer srcFile.Close()

	fileSize, _ := srcFile.Seek(0, 2)
	srcFile.Seek(0, 0)
	if isISO {
		logger.Printf("ISO: %d bytes (%.1f MB) — using absolute sector offsets", fileSize, float64(fileSize)/(1024*1024))
	} else {
		logger.Printf("TUNARIA.ESF: %d bytes (%.1f MB)", fileSize, float64(fileSize)/(1024*1024))
	}
	logger.Printf("Serving TUNARIA reads... (Ctrl+C to stop)")

	served := 0
	patchedReads := 0
	flagAddr := uint32(DebugBase + offRequestFlag)

	for {
		// Poll request flag.
		flag, err := p.ReadU32(flagAddr)
		if err != nil {
			time.Sleep(time.Millisecond)
			continue
		}
		if flag != flagPending {
			time.Sleep(time.Millisecond)
			continue
		}

		// Read request: sector, count, dest.
		reqBuf, err := p.Read(DebugBase, 0x0C)
		if err != nil || len(reqBuf) < 0x0C {
			p.WriteU32(flagAddr, flagError)
			continue
		}

		sector := binary.LittleEndian.Uint32(reqBuf[0x00:])
		count := binary.LittleEndian.Uint32(reqBuf[0x04:])
		dest := binary.LittleEndian.Uint32(reqBuf[0x08:])

		// Calculate byte offset in source.
		var byteOffset int64
		if isISO {
			byteOffset = int64(sector) * SectorSize
		} else {
			byteOffset = int64(sector-TunariaStartSector) * SectorSize
		}
		byteCount := int64(count) * SectorSize

		// Check patch overlaps.
		var data []byte
		patchTag := ""
		reqEnd := byteOffset + byteCount

		for i := range patches {
			pOff := patchOffset(patches[i], isISO)
			pEnd := pOff + int64(patches[i].Size)

			if byteOffset >= pOff && reqEnd <= pEnd {
				// Fully contained in patch — serve from patch.
				localOff := byteOffset - pOff
				data = patches[i].Data[localOff : localOff+byteCount]
				if int64(len(data)) != byteCount {
					logger.Printf("  WARN: patch data size mismatch: got %d, expected %d", len(data), byteCount)
					data = nil
					break
				}
				patchTag = fmt.Sprintf(" [ZONE %d]", patches[i].Zone)
				patchedReads++
				break
			} else if byteOffset < pEnd && reqEnd > pOff {
				// Partial overlap — use original (don't mix sources).
				patchTag = fmt.Sprintf(" [ZONE %d boundary, using original]", patches[i].Zone)
				break
			}
		}

		if data == nil {
			// No patch — read from original file.
			buf := make([]byte, byteCount)
			n, err := srcFile.ReadAt(buf, byteOffset)
			if err != nil || n == 0 {
				logger.Printf("  ERROR: read 0 bytes at offset 0x%x: %v", byteOffset, err)
				p.WriteU32(flagAddr, flagError)
				continue
			}
			data = buf[:n]
		}

		// Validate destination is within EE RAM.
		if dest >= EERAMSize || uint64(dest)+uint64(len(data)) > EERAMSize {
			logger.Printf("  ERROR: dest 0x%08X + %d bytes exceeds EE RAM!", dest, len(data))
			p.WriteU32(flagAddr, flagError)
			continue
		}

		// Write data to EE RAM.
		if err := p.Write(dest, data); err != nil {
			logger.Printf("  ERROR: writing to EE RAM: %v", err)
			p.WriteU32(flagAddr, flagError)
			continue
		}

		// Store transfer size and set flag = done.
		xferBuf := make([]byte, 4)
		binary.LittleEndian.PutUint32(xferBuf, uint32(len(data)))
		p.Write(DebugBase+offLastXferSize, xferBuf)
		p.WriteU32(flagAddr, flagDone)

		served++
		mbOff := float64(byteOffset) / (1024 * 1024)
		worldTag := worldName(sector)
		logger.Printf("  [%d] %s sector %d (%d sectors, %d bytes) → 0x%08X (offset %.1f MB)%s",
			served, worldTag, sector, count, len(data), dest, mbOff, patchTag)
	}
}

// LoadZonePatches loads zone overlay patches from a directory.
// Each patch is a pair: zone_N.json (metadata) + zone_N.bin (data).
func LoadZonePatches(dir string, isISO bool) ([]ZonePatch, error) {
	metaFiles, err := filepath.Glob(filepath.Join(dir, "zone_*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(metaFiles)

	var patches []ZonePatch
	for _, metaPath := range metaFiles {
		raw, err := os.ReadFile(metaPath)
		if err != nil {
			log.Printf("  WARNING: reading %s: %v", metaPath, err)
			continue
		}

		var meta struct {
			Zone          int   `json:"zone"`
			ZoneIndex     int   `json:"zone_index"`
			ISOByteOffset int64 `json:"iso_byte_offset"`
			TunByteOffset int64 `json:"tun_byte_offset"`
			FileOffset    int64 `json:"file_offset"`
			Size          int   `json:"size"`
		}
		if err := json.Unmarshal(raw, &meta); err != nil {
			log.Printf("  WARNING: parsing %s: %v", metaPath, err)
			continue
		}

		binPath := metaPath[:len(metaPath)-5] + ".bin" // .json → .bin
		data, err := os.ReadFile(binPath)
		if err != nil {
			log.Printf("  WARNING: %s not found, skipping", binPath)
			continue
		}
		if len(data) != meta.Size {
			log.Printf("  WARNING: %s size mismatch (%d vs %d), skipping", binPath, len(data), meta.Size)
			continue
		}

		zone := meta.Zone
		if zone == 0 {
			zone = meta.ZoneIndex
		}

		isoOff := meta.ISOByteOffset
		if isoOff == 0 {
			isoOff = meta.FileOffset
		}
		tunOff := meta.TunByteOffset
		if tunOff == 0 {
			tunOff = meta.FileOffset
		}

		patches = append(patches, ZonePatch{
			Zone:          zone,
			ISOByteOffset: isoOff,
			TunByteOffset: tunOff,
			Size:          meta.Size,
			Data:          data,
		})
	}
	return patches, nil
}

func patchOffset(patch ZonePatch, isISO bool) int64 {
	if isISO {
		return patch.ISOByteOffset
	}
	return patch.TunByteOffset
}

func isISOFile(path string) bool {
	ext := filepath.Ext(path)
	return ext == ".iso" || ext == ".ISO"
}

// worldName returns the world ESF name for a given sector.
func worldName(sector uint32) string {
	switch {
	case sector >= PlaneSkyStartSector && sector < PlaneSkyEndSector:
		return "PLANESKY"
	case sector >= OdusStartSector && sector < OdusEndSector:
		return "ODUS"
	case sector >= TunariaStartSector && sector < TunariaEndSector:
		return "TUNARIA"
	default:
		return "OTHER"
	}
}
