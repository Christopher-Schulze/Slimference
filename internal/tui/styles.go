// Package tui implements the BubbleTea TUI dashboard for Slimference.
package tui

import "github.com/charmbracelet/lipgloss"

// Palette defines the color scheme for the Slimference dashboard.
// We use a dark terminal palette with purple accent, green for savings,
// and subtle grays for secondary information.
var (
	// Accent colors.
	colorPurple    = lipgloss.Color("99")   // main accent - border/title
	colorGreen     = lipgloss.Color("78")   // good/savings/on
	colorGreenDim  = lipgloss.Color("34")   // dimmer green for bars
	colorOrange    = lipgloss.Color("215")  // warning
	colorRed       = lipgloss.Color("203")  // error
	colorBlue      = lipgloss.Color("75")   // info/provider indicator
	colorCyan      = lipgloss.Color("87")   // highlight
	colorGold      = lipgloss.Color("220")  // title/warm accent

	// Neutral palette.
	colorWhite    = lipgloss.Color("255")
	colorDimWhite = lipgloss.Color("250")
	colorGray     = lipgloss.Color("244")
	colorDimGray  = lipgloss.Color("240")
	colorDark     = lipgloss.Color("235")
)

// Styles are the pre-built lipgloss styles used throughout the TUI.
type Styles struct {
	// Container.
	Border        lipgloss.Style
	BorderActive  lipgloss.Style

	// Typography.
	Title         lipgloss.Style
	Subtitle      lipgloss.Style
	SectionHeader lipgloss.Style
	Normal        lipgloss.Style
	Dim           lipgloss.Style
	Muted         lipgloss.Style

	// Status indicators.
	OnBadge   lipgloss.Style
	OffBadge  lipgloss.Style
	Dot       lipgloss.Style
	DotOff    lipgloss.Style

	// Data emphasis.
	Saved     lipgloss.Style
	Highlight lipgloss.Style
	Warning   lipgloss.Style
	Error     lipgloss.Style

	// Table elements.
	TableHeader  lipgloss.Style
	TableCell    lipgloss.Style
	TableBorder  lipgloss.Style

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
}

// NewStyles builds the complete style set.
func NewStyles() Styles {
	return Styles{
		Border: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPurple).
			Padding(0, 1),

		BorderActive: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorCyan).
			Padding(0, 1),

		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(colorGold),

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
			Foreground(colorCyan),

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
			Foreground(colorPurple).
			Bold(true),

		FooterDesc: lipgloss.NewStyle().
			Foreground(colorDimGray),

		Flash: lipgloss.NewStyle().
			Foreground(colorGold).
			Bold(true),

		// Layout.
		PanelTitle: lipgloss.NewStyle().
			Foreground(colorPurple).
			Bold(true),

		Divider: lipgloss.NewStyle().
			Foreground(colorDimGray),

		HorizRule: lipgloss.NewStyle().
			Foreground(colorDimGray),

		HeaderBar: lipgloss.NewStyle().
			Background(lipgloss.Color("236")).
			Foreground(colorGold).
			Bold(true),

		// Keyboard hints.
		Key: lipgloss.NewStyle().
			Foreground(colorPurple).
			Bold(true),

		KeySep: lipgloss.NewStyle().
			Foreground(colorDimGray),

		// Big emphasis.
		BigSaved: lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true),

		// Setup instructions.
		SetupCmd: lipgloss.NewStyle().
			Foreground(colorCyan).
			Background(lipgloss.Color("236")).
			Padding(0, 1),

		SetupTitle: lipgloss.NewStyle().
			Foreground(colorGold).
			Bold(true),
	}
}
