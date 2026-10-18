package cast

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"math"
	"testing"
)

func TestValidateRejectsUnsafeAssetsAndParameters(t *testing.T) {
	for _, asset := range []string{"https://example.com/a.png", "file:///a.png", "data:image/svg+xml;base64,PHN2Zz4=", "data:image/png;base64,YmFk"} {
		c := Card{Kind: "character", Name: "C", Portrait: asset}
		if c.Validate() == nil {
			t.Fatalf("accepted %q", asset)
		}
	}
	for _, value := range []float64{-1, 3, math.NaN(), math.Inf(1)} {
		c := Card{Kind: "character", Name: "C", Temperature: &value}
		if c.Validate() == nil {
			t.Fatalf("accepted temperature %v", value)
		}
	}
	c := Card{Kind: "persona", Name: "P", Macros: map[string]string{"char": "override"}}
	if c.Validate() == nil {
		t.Fatal("accepted reserved macro")
	}
	c = Card{Kind: "persona", Name: "P", TextColor: "red; background:url(https://example.com)"}
	if c.Validate() == nil {
		t.Fatal("accepted CSS")
	}
}

func TestValidateImageFullyDecodesRaster(t *testing.T) {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	asset := func(raw []byte) string { return "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw) }
	if err := ValidateImage(asset(b.Bytes())); err != nil {
		t.Fatal(err)
	}
	// DecodeConfig accepts a PNG cut off after IHDR, but the image is unusable.
	if ValidateImage(asset(b.Bytes()[:33])) == nil {
		t.Fatal("accepted truncated raster")
	}
}
