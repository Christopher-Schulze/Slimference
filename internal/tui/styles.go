// Package tui implements the BubbleTea TUI dashboard for Slimference.
package tui

import "github.com/charmbracelet/lipgloss"

// Palette defines the color scheme for the Slimference dashboard.
// The direction is a dense operator console: dark steel surfaces, cyan focus,
// lime savings, and restrained amber warnings.
var (
	// Accent colors.
	colorAccent   = lipgloss.Color("81")  // focus and active borders
	colorGreen    = lipgloss.Color("78")  // good/savings/on
	colorGreenDim = lipgloss.Color("42")  // dimmer green for bars
	colorOrange   = lipgloss.Color("215") // warning
	colorRed      = lipgloss.Color("203") // error
	colorBlue     = lipgloss.Color("111") // info/provider indicator
	colorGold     = lipgloss.Color("221") // title/warm accent

	// Neutral palette.
	colorWhite    = lipgloss.Color("255")
	colorDimWhite = lipgloss.Color("250")
	colorGray     = lipgloss.Color("244")
	colorDimGray  = lipgloss.Color("240")
	colorDark     = lipgloss.Color("234")
	colorPanel    = lipgloss.Color("236")
	colorPanelAlt = lipgloss.Color("238")
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

// NewStyles builds the complete style set.
func NewStyles() Styles {
	return Styles{
		Border: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPanelAlt).
			Background(colorDark).
			Padding(0, 1),

		BorderActive: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Background(colorDark).
			Padding(0, 1),

		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWhite),

		Subtitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(colorDimWhite),

		SectionHeader: lipgloss.NewStyle().
			Foreground(colorDimGray).
			Bold(false),

		Normal: lipgloss.NewStyle().
			Foreground(colorDimWhite),

		Dim: lipgloss.NewStyle().
			Foreground(colorGray),

		Muted: lipgloss.NewStyle().
			Foreground(colorDimGray),

		OnBadge: lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true),

		OffBadge: lipgloss.NewStyle().
			Foreground(colorDimGray),

		Dot: lipgloss.NewStyle().
			Foreground(colorGreen),

		DotOff: lipgloss.NewStyle().
			Foreground(colorDimGray),

		Saved: lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true),

		Highlight: lipgloss.NewStyle().
			Foreground(colorAccent),

		Warning: lipgloss.NewStyle().
			Foreground(colorOrange),

		Error: lipgloss.NewStyle().
			Foreground(colorRed),

		TableHeader: lipgloss.NewStyle().
			Foreground(colorGray).
			Bold(true),

		TableCell: lipgloss.NewStyle().
			Foreground(colorDimWhite),

		TableBorder: lipgloss.NewStyle().
			Foreground(colorDimGray),

		BarFilled: lipgloss.NewStyle().
			Foreground(colorGreenDim),

		BarEmpty: lipgloss.NewStyle().
			Foreground(colorDark),

		LogTime: lipgloss.NewStyle().
			Foreground(colorDimGray),

		LogInfo: lipgloss.NewStyle().
			Foreground(colorBlue).
			Bold(true),

		LogWarn: lipgloss.NewStyle().
			Foreground(colorOrange).
			Bold(true),

		LogError: lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true),

		LogDebug: lipgloss.NewStyle().
			Foreground(colorDimGray),

		LogField: lipgloss.NewStyle().
			Foreground(colorGray),

		Footer: lipgloss.NewStyle().
			Foreground(colorDimGray),

		FooterKey: lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true),

		FooterDesc: lipgloss.NewStyle().
			Foreground(colorDimGray),

		Flash: lipgloss.NewStyle().
			Foreground(colorGold).
			Bold(true),

		// Layout.
		PanelTitle: lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true),

		Divider: lipgloss.NewStyle().
			Foreground(colorDimGray),

		HorizRule: lipgloss.NewStyle().
			Foreground(colorDimGray),

		HeaderBar: lipgloss.NewStyle().
			Background(colorPanel).
			Foreground(colorWhite).
			Bold(true),

		// Keyboard hints.
		Key: lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true),

		KeySep: lipgloss.NewStyle().
			Foreground(colorDimGray),

		// Big emphasis.
		BigSaved: lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true),

		// Setup instructions.
		SetupCmd: lipgloss.NewStyle().
			Foreground(colorAccent).
			Background(colorPanel).
			Padding(0, 1),

		SetupTitle: lipgloss.NewStyle().
			Foreground(colorGold).
			Bold(true),

		Card: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPanelAlt).
			Background(colorPanel).
			Padding(0, 1),

		CardActive: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Background(colorDark).
			Padding(0, 1),

		TabActive: lipgloss.NewStyle().
			Foreground(colorDark).
			Background(colorAccent).
			Bold(true).
			Padding(0, 1),

		TabIdle: lipgloss.NewStyle().
			Foreground(colorDimWhite).
			Background(colorPanel).
			Padding(0, 1),

		BannerGood: lipgloss.NewStyle().
			Foreground(colorDark).
			Background(colorGreen).
			Bold(true).
			Padding(0, 1),

		BannerWarn: lipgloss.NewStyle().
			Foreground(colorDark).
			Background(colorOrange).
			Bold(true).
			Padding(0, 1),

		StepDone: lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true),

		StepCursor: lipgloss.NewStyle().
			Foreground(colorDark).
			Background(colorAccent).
			Bold(true).
			Padding(0, 1),

		StepIdle: lipgloss.NewStyle().
			Foreground(colorDimWhite),

		StepIndex: lipgloss.NewStyle().
			Foreground(colorGray).
			Bold(true),

		Shortcut: lipgloss.NewStyle().
			Foreground(colorAccent).
			Background(colorPanel).
			Padding(0, 1),

		MenuGroup: lipgloss.NewStyle().
			Foreground(colorGray).
			Bold(true),

		MenuIdle: lipgloss.NewStyle().
			Foreground(colorDimWhite),

		MenuActive: lipgloss.NewStyle().
			Foreground(colorWhite).
			Background(colorPanelAlt).
			Bold(true).
			Padding(0, 1),

		MenuMeta: lipgloss.NewStyle().
			Foreground(colorDimGray),

		MenuOn: lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true),

		MenuOff: lipgloss.NewStyle().
			Foreground(colorDimGray),

		MenuWarn: lipgloss.NewStyle().
			Foreground(colorOrange).
			Bold(true),

		MetricKey: lipgloss.NewStyle().
			Foreground(colorGray),

		MetricVal: lipgloss.NewStyle().
			Foreground(colorWhite).
			Bold(true),
	}
}
