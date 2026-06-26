package codex

import (
	"fmt"
)

// Binary byte units (powers of 1024).
const (
	KiB = 1 << (10 * (iota + 1))
	MiB
	GiB
	TiB
)

// HumanBytes formats a byte count as a short human-readable string using
// binary units (KiB/MiB/GiB/TiB). Values below 1 KiB are shown in bytes.
//
// Examples: 0 -> "0 B", 512 -> "512 B", 1536 -> "1.5 KiB", 1<<20 -> "1.0 MiB".
func HumanBytes(n int64) string {
	switch {
	case n < KiB:
		return fmt.Sprintf("%d B", n)
	case n < MiB:
		return fmt.Sprintf("%.1f KiB", float64(n)/KiB)
	case n < GiB:
		return fmt.Sprintf("%.1f MiB", float64(n)/MiB)
	case n < TiB:
		return fmt.Sprintf("%.1f GiB", float64(n)/GiB)
	default:
		return fmt.Sprintf("%.1f TiB", float64(n)/TiB)
	}
}
