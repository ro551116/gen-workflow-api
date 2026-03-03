package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"strings"
)

// MaskRegion defines a rectangular area to be masked (white = static/frozen).
type MaskRegion struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// getImageDimensions fetches image header (URL or data URI) and returns width/height.
func getImageDimensions(imageURL string) (int, int, error) {
	var reader io.Reader

	if strings.HasPrefix(imageURL, "data:") {
		idx := strings.Index(imageURL, ",")
		if idx < 0 {
			return 0, 0, fmt.Errorf("invalid data URI")
		}
		decoded, err := base64.StdEncoding.DecodeString(imageURL[idx+1:])
		if err != nil {
			return 0, 0, fmt.Errorf("decode base64: %w", err)
		}
		reader = bytes.NewReader(decoded)
	} else {
		resp, err := http.Get(imageURL)
		if err != nil {
			return 0, 0, fmt.Errorf("fetch image: %w", err)
		}
		defer resp.Body.Close()
		reader = resp.Body
	}

	config, _, err := image.DecodeConfig(reader)
	if err != nil {
		return 0, 0, fmt.Errorf("decode image config: %w", err)
	}
	return config.Width, config.Height, nil
}

// generateMaskDataURI creates a mask PNG as a base64 data URI.
// Black background (dynamic/moving) with white rectangles (static/frozen).
func generateMaskDataURI(width, height int, regions []MaskRegion) (string, error) {
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	// Fill black (areas that will move)
	black := color.RGBA{0, 0, 0, 255}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, black)
		}
	}

	// Draw white rectangles (areas that stay frozen — text, logos)
	white := color.RGBA{255, 255, 255, 255}
	for _, r := range regions {
		for y := r.Y; y < r.Y+r.H && y < height; y++ {
			for x := r.X; x < r.X+r.W && x < width; x++ {
				if x >= 0 && y >= 0 {
					img.SetRGBA(x, y, white)
				}
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", fmt.Errorf("encode mask PNG: %w", err)
	}

	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	return fmt.Sprintf("data:image/png;base64,%s", b64), nil
}
