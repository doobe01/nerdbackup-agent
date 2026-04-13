package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// makeSolidIcon generates a valid 16x16 PNG filled with a single RGBA color.
// These are placeholder icons — replace with proper NerdBackup branded icons
// by swapping in //go:embed directives pointing to real PNG assets.
func makeSolidIcon(r, g, b uint8, size int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	c := color.RGBA{R: r, G: g, B: b, A: 255}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

var (
	iconOnline  = makeSolidIcon(0x22, 0xC5, 0x5E, 16) // Emerald green — agent connected
	iconOffline = makeSolidIcon(0x6B, 0x72, 0x80, 16) // Gray — agent not running / disconnected
	iconBusy    = makeSolidIcon(0xF5, 0x9E, 0x0B, 16) // Amber — backup in progress
)
