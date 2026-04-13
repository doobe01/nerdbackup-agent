//go:build !cgo

package tray

// Run is a no-op when CGO is disabled (headless builds).
// The system tray requires native platform APIs via CGO.
func Run(version string) {
	// No-op: tray not available without CGO
}
