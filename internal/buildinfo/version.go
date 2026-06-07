package buildinfo

// Version is the canonical Slimference build version.
// It can be overridden at build time via:
// go build -ldflags "-X github.com/slimference/slimference/internal/buildinfo.Version=0.9.1"
var Version = "0.9.1"
