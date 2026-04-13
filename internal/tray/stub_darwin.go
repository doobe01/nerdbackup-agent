//go:build darwin && !cgo

package tray

// Run is a no-op on macOS without CGO.
// macOS systray requires Cocoa via CGO. On macOS builds without CGO,
// the tray silently does nothing. Users with Xcode installed get the
// full tray experience.
func Run(version string) {}
