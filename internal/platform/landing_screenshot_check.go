package platform

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
	"image/png"
	"time"
)

type LandingScreenshotCheckResult struct {
	Browser string
	Width   int
	Height  int
	Sandbox bool
}

// CheckLandingScreenshot exercises the same isolated renderer used by the cover
// worker, without loading application credentials or opening a database. Run it
// as the account that starts the server to check executable, fonts, temporary
// directories, sandbox startup, JavaScript and screenshot output together.
func CheckLandingScreenshot(ctx context.Context) (LandingScreenshotCheckResult, error) {
	var result LandingScreenshotCheckResult
	var cfg Config
	if err := cfg.loadLandingScreenshotConfig(); err != nil {
		return result, err
	}
	browser, err := landingScreenshotBrowser(cfg.LandingScreenshotBrowser)
	if err != nil {
		return result, err
	}
	cfg.LandingScreenshotBrowser = browser
	server := &Server{Config: cfg, Now: func() time.Time { return time.Now().UTC() }}
	input := LandingCoverRenderInput{
		HTML:   `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>html,body{margin:0;min-height:100vh;background:rgb(190,20,70);color:white;font:24px sans-serif}h1{padding:32px}</style></head><body><h1>落地页预览 · Preview ready</h1><script>document.body.style.background='rgb(12,110,145)'</script></body></html>`,
		Config: DIYLandingOptions{Targets: []string{"apk", "pwa"}, PlatformButton: true},
		Name:   "Landing screenshot check", Mode: "pwa",
	}
	data, err := server.captureLandingScreenshot(ctx, input)
	if err != nil {
		return result, err
	}
	picture, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return result, fmt.Errorf("landing screenshot check returned an invalid PNG: %w", err)
	}
	pixel := color.NRGBAModel.Convert(picture.At(landingCoverWidth/2, landingCoverHeight/2)).(color.NRGBA)
	if pixel != (color.NRGBA{R: 12, G: 110, B: 145, A: 255}) {
		return result, fmt.Errorf("landing screenshot check did not render the expected CSS and JavaScript")
	}
	return LandingScreenshotCheckResult{Browser: browser, Width: picture.Bounds().Dx(), Height: picture.Bounds().Dy(), Sandbox: !cfg.LandingScreenshotNoSandbox}, nil
}
