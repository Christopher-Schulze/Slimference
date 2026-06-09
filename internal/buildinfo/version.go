package buildinfo

// Version is the canonical Slimference build version.
// It can be overridden at build time via:
// go build -ldflags "-X github.com/Christopher-Schulze/Slimference/internal/buildinfo.Version=0.6.0"
var Version = "0.6.0"
