package platform

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLandingScreenshotRequestIsolation(t *testing.T) {
	origin, document, assets := "http://127.0.0.1:18999", "/document/nonce", "/assets/nonce"
	for _, tc := range []struct {
		url, method string
		allowed     bool
	}{
		{origin + document, "GET", true}, {origin + document, "HEAD", true},
		{origin + assets + "?url=https%3A%2F%2Fexample.com%2Flogo.png&sig=abc", "GET", true},
		{origin + document + "?charge=1", "GET", false}, {origin + assets, "GET", false},
		{origin + document + "#fragment", "GET", false}, {origin + document, "POST", false},
		{origin + "/api/billing", "GET", false}, {origin + "/a/slug/download", "GET", false},
		{"http://localhost:18999" + document, "GET", false}, {"http://127.0.0.1:19000" + document, "GET", false},
		{"http://user@127.0.0.1:18999" + document, "GET", false},
		{"https://example.com/logo.png", "GET", false}, {"file:///etc/passwd", "GET", false},
		{"data:text/html,hello", "GET", false}, {"http://169.254.169.254/latest", "GET", false},
	} {
		t.Run(tc.method+tc.url, func(t *testing.T) {
			if got := landingScreenshotRequestAllowed(tc.url, tc.method, origin, document, assets); got != tc.allowed {
				t.Fatalf("allowed=%v; want %v", got, tc.allowed)
			}
		})
	}
}

func TestLandingScreenshotCDPPipe(t *testing.T) {
	responseReader, responseWriter := io.Pipe()
	commandReader, commandWriter := io.Pipe()
	p := newScreenshotCDP(responseReader, commandWriter)
	defer p.close()
	defer responseWriter.Close()
	defer commandReader.Close()
	serverErr := make(chan error, 1)
	go func() {
		raw, err := bufio.NewReader(commandReader).ReadBytes(0)
		if err != nil {
			serverErr <- err
			return
		}
		var command screenshotCDPMessage
		if err = json.Unmarshal(bytes.TrimSuffix(raw, []byte{0}), &command); err != nil {
			serverErr <- err
			return
		}
		if command.Method != "Page.test" || command.SessionID != "session" {
			serverErr <- io.ErrUnexpectedEOF
			return
		}
		event, _ := json.Marshal(screenshotCDPMessage{SessionID: "session", Method: "Page.event", Params: json.RawMessage(`{"ok":true}`)})
		_, err = responseWriter.Write(append(event, 0))
		if err != nil {
			serverErr <- err
			return
		}
		reply, _ := json.Marshal(screenshotCDPMessage{ID: command.ID, Result: json.RawMessage(`{"answer":42}`)})
		_, err = responseWriter.Write(append(reply, 0))
		serverErr <- err
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var reply struct{ Answer int }
	if err := p.call(ctx, "session", "Page.test", map[string]any{"enabled": true}, &reply); err != nil || reply.Answer != 42 {
		t.Fatalf("reply=%+v err=%v", reply, err)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-p.events:
		if event.Method != "Page.event" {
			t.Fatal(event.Method)
		}
	case <-ctx.Done():
		t.Fatal("event lost")
	}
	cancelled, cancelNow := context.WithCancel(context.Background())
	cancelNow()
	if err := p.call(cancelled, "", "Never.write", nil, nil); err != context.Canceled {
		t.Fatalf("cancel error: %v", err)
	}
	p.close()
	if err := p.call(context.Background(), "", "Never.write", nil, nil); err != io.EOF {
		t.Fatalf("close error: %v", err)
	}
}

func TestLandingScreenshotBrowserEnvironmentExcludesSecrets(t *testing.T) {
	t.Setenv("D1_SCREENSHOT_TEST_SECRET", "do-not-pass")
	for _, v := range screenshotBrowserEnvironment() {
		if strings.Contains(v, "do-not-pass") || strings.HasPrefix(v, "D1_SCREENSHOT_TEST_SECRET=") {
			t.Fatal("server secret leaked to browser environment")
		}
	}
}

// Opt in on hosts with Chromium. This checks actual pixels after custom CSS,
// JavaScript, inline image decoding and delayed layout, plus outbound isolation.
func TestLandingScreenshotChromium(t *testing.T) {
	browser := os.Getenv("TEST_LANDING_SCREENSHOT_BROWSER")
	if browser == "" {
		t.Skip("set TEST_LANDING_SCREENSHOT_BROWSER to run real Chromium capture")
	}
	var businessHits atomic.Int32
	business := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { businessHits.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer business.Close()
	s := &Server{Config: Config{LandingScreenshotBrowser: browser, LandingScreenshotTimeout: 20 * time.Second, Secret: "screenshot-test-only"}, Now: time.Now}
	asset := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			asset.SetNRGBA(x, y, color.NRGBA{R: 250, G: 200, B: 50, A: 255})
		}
	}
	var assetPNG bytes.Buffer
	if err := png.Encode(&assetPNG, asset); err != nil {
		t.Fatal(err)
	}
	imageHTML := `<img alt="" style="position:fixed;left:40px;top:100px;width:32px;height:32px;z-index:1" src="data:image/png;base64,` + base64.StdEncoding.EncodeToString(assetPNG.Bytes()) + `">`
	for _, viewport := range []string{`<meta name="viewport" content="width=device-width,initial-scale=1">`, ""} {
		t.Run(map[bool]string{true: "mobile-viewport", false: "source-without-viewport"}[viewport != ""], func(t *testing.T) {
			input := LandingCoverRenderInput{HTML: `<!doctype html><html><head>` + viewport + `<style>html,body{margin:0;min-height:100%;background:rgb(15,65,95)}#hero{height:320px;background:rgb(240,180,40)}#color{position:fixed;inset:0;background:rgb(190,20,70)}</style></head><body><main id="hero"><h1>真实落地页封面</h1><button data-d1-action="primary">Install</button></main><div id="color"></div><script>setTimeout(()=>{document.querySelector('#color').style.background='rgb(12,110,145)'},100);fetch('` + business.URL + `/billing',{method:'POST'}).catch(()=>{});new Image().src='` + business.URL + `/download'</script></body></html>`, Config: DIYLandingOptions{Targets: []string{"apk", "pwa"}, PlatformButton: true}, Name: "Real screenshot", Mode: "pwa"}
			input.HTML = strings.Replace(input.HTML, "</body>", imageHTML+"</body>", 1)
			data, err := s.captureLandingScreenshot(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			picture, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if picture.Bounds().Dx() != 390 || picture.Bounds().Dy() != 844 {
				t.Fatalf("dimensions: %v", picture.Bounds())
			}
			for _, point := range [][2]int{{10, 10}, {195, 420}, {380, 830}} {
				got := color.NRGBAModel.Convert(picture.At(point[0], point[1])).(color.NRGBA)
				if got.R != 12 || got.G != 110 || got.B != 145 || got.A != 255 {
					t.Fatalf("CSS/JS render pixel %v=%v", point, got)
				}
			}
			if viewport != "" {
				got := color.NRGBAModel.Convert(picture.At(50, 110)).(color.NRGBA)
				if got.R != 250 || got.G != 200 || got.B != 50 {
					t.Fatalf("real inline image missing from cover: %v", got)
				}
			}
		})
	}
	t.Run("automatic-navigation-is-blocked", func(t *testing.T) {
		input := LandingCoverRenderInput{HTML: `<button data-d1-action="primary">Install</button><script>location.href='` + business.URL + `/return-flow'</script>`, Config: DIYLandingOptions{PlatformButton: true}, Mode: "apk", Name: "Blocked navigation"}
		if data, err := s.captureLandingScreenshot(context.Background(), input); err == nil || len(data) != 0 {
			t.Fatal("redirect/error page was incorrectly published as a real cover")
		}
	})
	t.Run("stalled-script-is-cancelled", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		started := time.Now()
		input := LandingCoverRenderInput{HTML: `<button data-d1-action="primary">Install</button><script>while(true){}</script>`, Config: DIYLandingOptions{PlatformButton: true}, Mode: "apk", Name: "Bounded capture"}
		if data, err := s.captureLandingScreenshot(ctx, input); err == nil || len(data) != 0 {
			t.Fatal("stalled screenshot was not cancelled")
		}
		if time.Since(started) > 4*time.Second {
			t.Fatal("browser cleanup exceeded cancellation bound")
		}
	})
	if businessHits.Load() != 0 {
		t.Fatalf("screenshot contacted business endpoints %d times", businessHits.Load())
	}
}
