package report

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// The one palette, chosen for a dark terminal. Green means safe, red means
// broken, amber means "look at this"; the accent is for blast's own chrome.
var (
	Accent = lipgloss.Color("#B79CFF")
	Green  = lipgloss.Color("#3DDC97")
	Red    = lipgloss.Color("#FF5C7A")
	Amber  = lipgloss.Color("#FFB454")
	Muted  = lipgloss.Color("#7A8194")
)

var (
	accent = lipgloss.NewStyle().Foreground(Accent)
	good   = lipgloss.NewStyle().Foreground(Green)
	bad    = lipgloss.NewStyle().Foreground(Red)
	warn   = lipgloss.NewStyle().Foreground(Amber)
	muted  = lipgloss.NewStyle().Foreground(Muted)
)

// The wordmark, in half-block letters.
var bannerArt = []string{
	"█▄▄ █   ▄▀█ █▀ ▀█▀",
	"█▄█ █▄▄ █▀█ ▄█  █ ",
}

// Banner is the wordmark, shaded from the accent through red to amber like a
// blast wave moving outward.
func Banner() string {
	shades := lipgloss.Blend1D(lipgloss.Width(bannerArt[0]), Accent, Red, Amber)
	var b strings.Builder
	for _, line := range bannerArt {
		b.WriteByte(' ')
		for i, r := range []rune(line) {
			b.WriteString(lipgloss.NewStyle().Foreground(shades[i]).Render(string(r)))
		}
		b.WriteByte('\n')
	}
	return b.String()
}
