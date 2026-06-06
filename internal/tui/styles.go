// Package tui implements the BubbleTea TUI dashboard for Slimference.
package tui

import "github.com/charmbracelet/lipgloss"

// Palette is intentionally restricted: one solid dark background, one bright
// foreground, one warm accent for focus/active, plus three semantic colors
// (success/warn/error). Everything renders against the same dark surface, so
// the dashboard reads as one unified panel rather than a patchwork of
// background blocks.
var (
	colorBg      = lipgloss.Color("235") // solid dashboard background
	colorBgAlt   = lipgloss.Color("237") // subtle row separator (only used sparingly)
	colorFg      = lipgloss.Color("253") // primary foreground
	colorFgDim   = lipgloss.Color("245") // secondary foreground
	colorFgMuted = lipgloss.Color("240") // muted / structural
	colorAccent  = lipgloss.Color("216") // focus / cursor / active row
	colorGreen   = lipgloss.Color("114") // savings / health-ok
	colorOrange  = lipgloss.Color("179") // warning
	colorRed     = lipgloss.Color("203") // error

	// Compatibility aliases (kept so existing references compile while the
	// styles below switch to the reduced palette). New code should reach for
	// the names above.
	colorGreenDim = colorGreen
	colorBlue     = colorAccent
	colorGold     = colorFg
	colorWhite    = colorFg
	colorDimWhite = colorFg
	colorGray     = colorFgDim
	colorDimGray  = colorFgMuted
	colorDark     = colorBg
	colorPanel    = colorBg
	colorPanelAlt = colorBgAlt
)

// Styles are the pre-built lipgloss styles used throughout the TUI.
type Styles struct {
	// Container.
	Border       lipgloss.Style
	BorderActive lipgloss.Style

	// Typography.
	Title         lipgloss.Style
	Subtitle      lipgloss.Style
	SectionHeader lipgloss.Style
	Normal        lipgloss.Style
	Dim           lipgloss.Style
	Muted         lipgloss.Style

	// Status indicators.
	OnBadge  lipgloss.Style
	OffBadge lipgloss.Style
	Dot      lipgloss.Style
	DotOff   lipgloss.Style

	// Data emphasis.
	Saved     lipgloss.Style
	Highlight lipgloss.Style
	Warning   lipgloss.Style
	Error     lipgloss.Style

	// Table elements.
	TableHeader lipgloss.Style
	TableCell   lipgloss.Style
	TableBorder lipgloss.Style

	// Progress bar.
	BarFilled lipgloss.Style
	BarEmpty  lipgloss.Style

	// Log entries.
	LogTime  lipgloss.Style
	LogInfo  lipgloss.Style
	LogWarn  lipgloss.Style
	LogError lipgloss.Style
	LogDebug lipgloss.Style
	LogField lipgloss.Style

	// Footer.
	Footer     lipgloss.Style
	FooterKey  lipgloss.Style
	FooterDesc lipgloss.Style

	// Flash message.
	Flash lipgloss.Style

	// Layout.
	PanelTitle lipgloss.Style // section headers inside panels (PROVIDERS, LIVE ...)
	Divider    lipgloss.Style // │ vertical separator between columns
	HorizRule  lipgloss.Style // ─ horizontal separator lines
	HeaderBar  lipgloss.Style // top header bar background feel

	// Keyboard hints in footer.
	Key    lipgloss.Style // the key letter: [c]
	KeySep lipgloss.Style // · separator between key groups

	// Big emphasis.
	BigSaved lipgloss.Style // large % / token savings number

	// Setup instructions.
	SetupCmd   lipgloss.Style // $ slimference hook install claude
	SetupTitle lipgloss.Style // QUICK START heading
	Card       lipgloss.Style
	CardActive lipgloss.Style
	TabActive  lipgloss.Style
	TabIdle    lipgloss.Style
	BannerGood lipgloss.Style
	BannerWarn lipgloss.Style
	StepDone   lipgloss.Style
	StepCursor lipgloss.Style
	StepIdle   lipgloss.Style
	StepIndex  lipgloss.Style
	Shortcut   lipgloss.Style
	MenuGroup  lipgloss.Style
	MenuIdle   lipgloss.Style
	MenuActive lipgloss.Style
	MenuMeta   lipgloss.Style
	MenuOn     lipgloss.Style
	MenuOff    lipgloss.Style
	MenuWarn   lipgloss.Style
	MetricKey  lipgloss.Style
	MetricVal  lipgloss.Style
}

// NewStyles builds the complete style set. The dashboard renders entirely
// against `colorBg`; only the outer container plus the active-row marker
// carry a Background() so the surface reads as one solid panel. Everything
// else is foreground-only with bold/underline accents - no ad-hoc badge
// blocks, no rainbow.
func NewStyles() Styles {
	container := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorFgMuted).
		Background(colorBg).
		Foreground(colorFg).
		Padding(0, 1)

	containerActive := container.Copy().BorderForeground(colorAccent)

	plain := lipgloss.NewStyle().Background(colorBg).Foreground(colorFg)
	dim := lipgloss.NewStyle().Background(colorBg).Foreground(colorFgDim)
	muted := lipgloss.NewStyle().Background(colorBg).Foreground(colorFgMuted)
	accent := lipgloss.NewStyle().Background(colorBg).Foreground(colorAccent)
	good := lipgloss.NewStyle().Background(colorBg).Foreground(colorGreen)
	warn := lipgloss.NewStyle().Background(colorBg).Foreground(colorOrange)
	bad := lipgloss.NewStyle().Background(colorBg).Foreground(colorRed)

	return Styles{
		Border:       container,
		BorderActive: containerActive,

		Title:         plain.Copy().Bold(true),
		Subtitle:      dim.Copy().Bold(true),
		SectionHeader: muted.Copy(),
		Normal:        plain,
		Dim:           dim,
		Muted:         muted,

		OnBadge:  good.Copy().Bold(true),
		OffBadge: muted,
		Dot:      good,
		DotOff:   muted,

		Saved:     good.Copy().Bold(true),
		Highlight: accent,
		Warning:   warn,
		Error:     bad,

		TableHeader: dim.Copy().Bold(true),
		TableCell:   plain,
		TableBorder: muted,

		BarFilled: good,
		BarEmpty:  muted,

		LogTime:  muted,
		LogInfo:  accent.Copy().Bold(true),
		LogWarn:  warn.Copy().Bold(true),
		LogError: bad.Copy().Bold(true),
		LogDebug: muted,
		LogField: dim,

		Footer:     muted,
		FooterKey:  accent.Copy().Bold(true),
		FooterDesc: muted,

		Flash: accent.Copy().Bold(true),

		// Layout.
		PanelTitle: accent.Copy().Bold(true),
		Divider:    muted,
		HorizRule:  muted,
		HeaderBar:  plain.Copy().Bold(true),

		// Keyboard hints (rendered inline, no padding block).
		Key:    accent.Copy().Bold(true),
		KeySep: muted,

		// Big emphasis.
		BigSaved: good.Copy().Bold(true),

		// Setup instructions (no background block - just colored inline text).
		SetupCmd:   accent,
		SetupTitle: plain.Copy().Bold(true),

		Card:       container,
		CardActive: containerActive,

		// Menu / tab markers: foreground-only, cursor uses `▶` plus accent
		// color, idle is dim foreground. No Background() blocks.
		TabActive: accent.Copy().Bold(true),
		TabIdle:   dim,

		BannerGood: good.Copy().Bold(true),
		BannerWarn: warn.Copy().Bold(true),

		StepDone:   good.Copy().Bold(true),
		StepCursor: accent.Copy().Bold(true),
		StepIdle:   plain,
		StepIndex:  dim.Copy().Bold(true),
		Shortcut:   accent,

		MenuGroup:  dim.Copy().Bold(true),
		MenuIdle:   plain,
		MenuActive: accent.Copy().Bold(true),
		MenuMeta:   muted,
		MenuOn:     good.Copy().Bold(true),
		MenuOff:    muted,
		MenuWarn:   warn.Copy().Bold(true),

		MetricKey: dim,
		MetricVal: plain.Copy().Bold(true),
	}
}
