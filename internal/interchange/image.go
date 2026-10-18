// Package interchange parses and writes character-card and snapshot files.
// Original implementation: Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// The Tavern card (V1/V2/V3 JSON, PNG tEXt "chara"/"ccv3", CHARX) field names are
// the public community formats; GobboNet 16-card-io.js @
// 314bdc9cae204ae38fdc1002ac6a9fb1437c109f (Copyright (C) 2026 Elodine / GoblinCorps)
// is a functional reference only and no upstream source is copied.
// Everything parsed here is untrusted: sizes, entry counts and decompression are
// bounded, nothing is fetched or executed, and unmapped data stays inert.
// See docs/REUSE-LOG.md for the design/implementation boundary.
package interchange

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"

	"github.com/Labyricorn/Labbonet/internal/cast"
)

const maxImageBytes = 1 << 20

// normalizeImage turns untrusted PNG/JPEG bytes into a validated embedded data URI.
// PNG metadata (including embedded cards) is stripped by re-encoding, and images
// larger than 1 MiB are downscaled so they fit the cast image limit.
func normalizeImage(b []byte) (string, string, error) {
	if len(b) == 0 || len(b) > 16<<20 {
		return "", "", errors.New("image missing or larger than 16 MiB")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || (format != "png" && format != "jpeg") {
		return "", "", errors.New("image is not a valid PNG or JPEG")
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 || int64(cfg.Width)*int64(cfg.Height) > 16000000 {
		return "", "", errors.New("image dimensions exceed 4096 / 16 megapixels")
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return "", "", errors.New("image is invalid or truncated")
	}
	if format == "jpeg" && len(b) <= maxImageBytes {
		uri := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(b)
		return uri, "", cast.ValidateImage(uri)
	}
	note := ""
	for attempt := 0; attempt < 8; attempt++ {
		var buf bytes.Buffer
		if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, img); err != nil {
			return "", "", errors.New("image could not be re-encoded")
		}
		if buf.Len() <= 700*1024 { // base64 expansion must stay below the 1.4 MB data URI cap
			uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
			if err := cast.ValidateImage(uri); err != nil {
				return "", "", err
			}
			return uri, note, nil
		}
		img = shrink(img)
		note = "portrait was downscaled to fit the 1 MiB image limit"
	}
	return "", "", errors.New("image is too large to embed")
}

func shrink(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx()*7/10, b.Dy()*7/10
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(x, y, src.At(b.Min.X+x*b.Dx()/w, b.Min.Y+y*b.Dy()/h))
		}
	}
	return dst
}

// portraitPNG returns clean PNG bytes for export, converting JPEG or substituting
// a neutral placeholder when the card has no portrait.
func portraitPNG(dataURI string) ([]byte, error) {
	var img image.Image
	if dataURI != "" {
		_, payload, ok := strings.Cut(dataURI, ",")
		if !ok {
			return nil, errors.New("invalid portrait")
		}
		raw, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, errors.New("invalid portrait encoding")
		}
		img, _, err = image.Decode(bytes.NewReader(raw))
		if err != nil {
			return nil, errors.New("invalid portrait")
		}
	} else {
		p := image.NewNRGBA(image.Rect(0, 0, 256, 256))
		for y := 0; y < 256; y++ {
			for x := 0; x < 256; x++ {
				p.Set(x, y, color.NRGBA{R: 24, G: 28, B: uint8(40 + y/8), A: 255})
			}
		}
		img = p
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var _ = jpeg.Decode // registers the JPEG decoder for image.Decode
