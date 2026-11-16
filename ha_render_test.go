package main

import (
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"strings"
	"testing"
)

// Guards a real first-test bug: the token was spliced in as a bare object, which
// JS coerces to "[object Object]". The setItem argument must be a JS string
// literal that itself holds JSON.
func TestHassTokensJSIsStringLiteralOfJSON(t *testing.T) {
	const token = `tok"en\with'quotes`
	js, err := hassTokensJS("http://ha.local:8123", token)
	if err != nil {
		t.Fatalf("hassTokensJS: %v", err)
	}
	const prefix = `localStorage.setItem("hassTokens", `
	if !strings.HasPrefix(js, prefix) || !strings.HasSuffix(js, ")") {
		t.Fatalf("unexpected shape: %s", js)
	}
	literal := strings.TrimSuffix(strings.TrimPrefix(js, prefix), ")")

	var inner string
	if err := json.Unmarshal([]byte(literal), &inner); err != nil {
		t.Fatalf("argument is not a JS string literal: %v", err)
	}
	var tokens map[string]any
	if err := json.Unmarshal([]byte(inner), &tokens); err != nil {
		t.Fatalf("string literal does not hold JSON: %v", err)
	}
	if tokens["access_token"] != token {
		t.Errorf("access_token = %v", tokens["access_token"])
	}
	if tokens["hassUrl"] != "http://ha.local:8123" || tokens["clientId"] != "http://ha.local:8123/" {
		t.Errorf("url fields = %v / %v", tokens["hassUrl"], tokens["clientId"])
	}
}

// Guards the fmt.Sprintf template: an unescaped % in the JS shows up as %!.
func TestKioskModeJSFormatting(t *testing.T) {
	js := kioskModeJS(1024, 758)
	if i := strings.Index(js, "%!"); i >= 0 {
		t.Errorf("format artifact in output: %s", js[i:min(i+40, len(js))])
	}
	for _, want := range []string{"'1024px'", "'758px'", "r.right > 1024", "width = '100%'"} {
		if !strings.Contains(js, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestLeftStripBlank(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 100, 50))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.Gray{Y: 255}), image.Point{}, draw.Src)
	if !leftStripBlank(img, 8) {
		t.Error("all-white image must be blank")
	}
	img.SetGray(20, 10, color.Gray{Y: 0})
	if !leftStripBlank(img, 8) {
		t.Error("a dark pixel outside the strip must not matter")
	}
	img.SetGray(3, 10, color.Gray{Y: 0})
	if leftStripBlank(img, 8) {
		t.Error("a dark pixel inside the strip must fail the check")
	}
}
