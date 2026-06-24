// Package ui provides terminal color/emoji helpers for panelgen output.
package ui

import (
	"fmt"
	"os"
	"time"
)

// Enabled is true when stderr is an interactive terminal.
var Enabled = isTerminal(os.Stderr)

func isTerminal(f *os.File) bool {
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeCharDevice != 0
}

func apply(code, s string) string {
	if !Enabled {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}

func Bold(s string) string       { return apply("1", s) }
func Dim(s string) string        { return apply("2", s) }
func Green(s string) string      { return apply("32", s) }
func Yellow(s string) string     { return apply("33", s) }
func Red(s string) string        { return apply("31", s) }
func Cyan(s string) string       { return apply("36", s) }
func BoldGreen(s string) string  { return apply("1;32", s) }
func BoldYellow(s string) string { return apply("1;33", s) }
func BoldRed(s string) string    { return apply("1;31", s) }
func BoldCyan(s string) string   { return apply("1;36", s) }

// Sep returns a dimmed middle-dot separator for inline fields.
func Sep() string { return Dim(" · ") }

// Dur formats a duration compactly.
func Dur(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// Icons.
const (
	IconOK    = "✅"
	IconFail  = "❌"
	IconSkip  = "⏭️ "
	IconGen   = "🎨"
	IconWarn  = "⚠️ "
	IconPlan  = "📋"
	IconChar  = "🎭"
	IconScene = "🎬"
	IconDry   = "🔍"
)
