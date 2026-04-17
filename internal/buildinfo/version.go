package buildinfo

// Version is the canonical Slimference build version.
// It can be overridden at build time via:
// go build -ldflags "-X github.com/slimference/slimference/internal/buildinfo.Version=2.0.2"
var Version = "2.0.2"
