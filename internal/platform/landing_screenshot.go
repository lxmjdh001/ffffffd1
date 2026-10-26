package platform

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

const landingScreenshotWidth = landingCoverWidth
const landingScreenshotHeight = landingCoverHeight
const landingScreenshotMaxPNG = maxLandingCoverBytes

// A capture has its own browser profile and loopback-only document server. The
// browser never receives a console session, app installation token or API key.
// All outbound page requests are intercepted; only this prepared document and
// its signed, MIME-checked static resource proxy can reach the network.
func (s *Server) captureLandingScreenshot(parent context.Context, input LandingCoverRenderInput) ([]byte, error) {
	timeout := s.Config.LandingScreenshotTimeout
	if timeout == 0 {
		timeout = 25 * time.Second
	}
	if timeout < 10*time.Second || timeout > 60*time.Second {
		return nil, errors.New("封面截图超时配置无效")
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	browser, err := landingScreenshotBrowser(s.Config.LandingScreenshotBrowser)
	if err != nil {
		return nil, err
	}
	prepared, _, err := prepareDIYHTML(input.HTML, input.Config)
	if err != nil {
		return nil, err
	}
	if input.Mode != "apk" && input.Mode != "pwa" {
		return nil, errors.New("封面截图应用类型无效")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("封面截图服务无法启动")
	}
	origin := "http://" + listener.Addr().String()
	nonce := randomHex(24)
	documentPath := "/document/" + nonce
	assetPath := "/assets/" + nonce
	expires := s.Now().Add(2 * time.Minute)
	label := "安装应用"
	if input.Mode == "apk" {
		label = "下载应用"
	}
	document, err := diyContentDocument(prepared, input.Config, nonce, input.Mode, label, input.Name, assetPath, true, func(raw, kind string) string { return s.diyAssetURL(assetPath, raw, kind, expires) })
	if err != nil {
		listener.Close()
		return nil, err
	}
	var hits atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		if r.Host != listener.Addr().String() || (r.Method != http.MethodGet && r.Method != http.MethodHead) || hits.Add(1) > 200 {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Path {
		case documentPath:
			if r.URL.RawQuery != "" {
				http.NotFound(w, r)
				return
			}
			s.writeDIYContent(w, r, document, origin+assetPath)
		case assetPath:
			s.serveDIYAsset(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	httpServer := &http.Server{Handler: handler, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 2 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	go func() { _ = httpServer.Serve(listener) }()
	defer httpServer.Close()
	profile, err := os.MkdirTemp("", "d1-landing-screenshot-")
	if err != nil {
		return nil, errors.New("封面截图临时目录无法创建")
	}
	defer os.RemoveAll(profile)
	pipe, stop, err := startLandingScreenshotBrowser(ctx, browser, profile, s.Config.LandingScreenshotNoSandbox)
	if err != nil {
		return nil, err
	}
	defer stop()
	var target struct {
		TargetID string `json:"targetId"`
	}
	if err = pipe.call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"}, &target); err != nil {
		return nil, screenshotProtocolError(ctx, err)
	}
	var attachment struct {
		SessionID string `json:"sessionId"`
	}
	if err = pipe.call(ctx, "", "Target.attachToTarget", map[string]any{"targetId": target.TargetID, "flatten": true}, &attachment); err != nil {
		return nil, screenshotProtocolError(ctx, err)
	}
	session := attachment.SessionID
	_ = pipe.call(ctx, "", "Browser.setDownloadBehavior", map[string]any{"behavior": "deny"}, nil)
	var navigationCount atomic.Int32
	var blockedNavigation atomic.Bool
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-pipe.done:
				return
			case event := <-pipe.events:
				if event.SessionID != session {
					continue
				}
				switch event.Method {
				case "Fetch.requestPaused":
					var paused struct {
						RequestID    string `json:"requestId"`
						ResourceType string `json:"resourceType"`
						Request      struct {
							URL    string `json:"url"`
							Method string `json:"method"`
						} `json:"request"`
					}
					if json.Unmarshal(event.Params, &paused) != nil {
						continue
					}
					allow := landingScreenshotRequestAllowed(paused.Request.URL, paused.Request.Method, origin, documentPath, assetPath)
					if paused.ResourceType == "Document" && (paused.Request.URL != origin+documentPath || navigationCount.Add(1) != 1) {
						allow = false
						blockedNavigation.Store(true)
					}
					method := "Fetch.failRequest"
					params := map[string]any{"requestId": paused.RequestID, "errorReason": "BlockedByClient"}
					if allow {
						method = "Fetch.continueRequest"
						params = map[string]any{"requestId": paused.RequestID}
					}
					if pipe.call(ctx, session, method, params, nil) != nil {
						return
					}
				case "Page.javascriptDialogOpening":
					_ = pipe.call(ctx, session, "Page.handleJavaScriptDialog", map[string]any{"accept": false}, nil)
				}
			}
		}
	}()
	for _, command := range []struct {
		method string
		params any
	}{
		{"Page.enable", map[string]any{}},
		{"Runtime.enable", map[string]any{}},
		{"Fetch.enable", map[string]any{"patterns": []map[string]any{{"urlPattern": "*", "requestStage": "Request"}}}},
		{"Emulation.setDeviceMetricsOverride", map[string]any{"width": landingScreenshotWidth, "height": landingScreenshotHeight, "deviceScaleFactor": 1, "mobile": true, "screenWidth": landingScreenshotWidth, "screenHeight": landingScreenshotHeight}},
		{"Emulation.setTouchEmulationEnabled", map[string]any{"enabled": true}},
		{"Emulation.setEmulatedMedia", map[string]any{"features": []map[string]any{{"name": "prefers-reduced-motion", "value": "reduce"}}}},
	} {
		if err = pipe.call(ctx, session, command.method, command.params, nil); err != nil {
			return nil, screenshotProtocolError(ctx, err)
		}
	}
	var navigation struct {
		ErrorText string `json:"errorText"`
	}
	if err = pipe.call(ctx, session, "Page.navigate", map[string]any{"url": origin + documentPath}, &navigation); err != nil || navigation.ErrorText != "" {
		return nil, screenshotProtocolError(ctx, err)
	}
	quotedURL, _ := json.Marshal(origin + documentPath)
	readyExpression := `location.href===` + string(quotedURL) + `&&document.readyState==='complete'`
	readyDeadline := time.Now().Add(12 * time.Second)
	ready := false
	for !ready {
		if blockedNavigation.Load() {
			return nil, errors.New("页面跳转或视口异常，无法生成真实封面")
		}
		if time.Now().After(readyDeadline) {
			return nil, errors.New("落地页加载超时，请检查页面资源后重试")
		}
		var result screenshotEvaluation
		if pipe.call(ctx, session, "Runtime.evaluate", map[string]any{"expression": readyExpression, "returnByValue": true}, &result) == nil {
			_ = json.Unmarshal(result.Result.Value, &ready)
		}
		if ready {
			break
		}
		select {
		case <-ctx.Done():
			return nil, screenshotProtocolError(ctx, err)
		case <-pipe.done:
			return nil, screenshotProtocolError(ctx, err)
		case <-time.After(100 * time.Millisecond):
		}
	}
	// Wait for real fonts, image decoding, delayed layout and a fresh paint. No
	// synthetic page, illustration, download or installation state is introduced.
	settleExpression := `(async()=>{await Promise.race([Promise.all([document.fonts?.ready,...Array.from(document.images).map(i=>i.decode?i.decode().catch(()=>{}):Promise.resolve())]),new Promise(r=>setTimeout(r,2500))]);await new Promise(r=>setTimeout(r,600));await new Promise(r=>requestAnimationFrame(()=>requestAnimationFrame(r)));return {url:location.href,width:innerWidth,height:innerHeight};})()`
	var settled screenshotEvaluation
	if err = pipe.call(ctx, session, "Runtime.evaluate", map[string]any{"expression": settleExpression, "awaitPromise": true, "returnByValue": true, "timeout": 5000}, &settled); err != nil || len(settled.ExceptionDetails) > 0 {
		return nil, screenshotProtocolError(ctx, err)
	}
	var pageState struct {
		URL    string `json:"url"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	if blockedNavigation.Load() || json.Unmarshal(settled.Result.Value, &pageState) != nil || pageState.URL != origin+documentPath || pageState.Width <= 0 || pageState.Height <= 0 {
		return nil, errors.New("页面跳转或视口异常，无法生成真实封面")
	}
	var capture struct {
		Data string `json:"data"`
	}
	if err = pipe.call(ctx, session, "Page.captureScreenshot", map[string]any{"format": "png", "fromSurface": true, "captureBeyondViewport": false, "clip": map[string]any{"x": 0, "y": 0, "width": landingScreenshotWidth, "height": landingScreenshotHeight, "scale": 1}}, &capture); err != nil {
		return nil, screenshotProtocolError(ctx, err)
	}
	if len(capture.Data) > base64.StdEncoding.EncodedLen(landingScreenshotMaxPNG) {
		return nil, errors.New("生成的封面图片超过大小限制")
	}
	data, err := base64.StdEncoding.DecodeString(capture.Data)
	if err != nil || len(data) > landingScreenshotMaxPNG {
		return nil, errors.New("封面截图内容无效")
	}
	image, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || image.Width != landingScreenshotWidth || image.Height != landingScreenshotHeight {
		return nil, errors.New("封面截图内容无效")
	}
	return data, nil
}

type screenshotEvaluation struct {
	Result struct {
		Value json.RawMessage `json:"value"`
	} `json:"result"`
	ExceptionDetails json.RawMessage `json:"exceptionDetails"`
}

func screenshotProtocolError(ctx context.Context, cause error) error {
	if ctx.Err() != nil {
		return errors.New("封面截图超时，请稍后重试")
	}
	if cause != nil {
		return fmt.Errorf("浏览器截图生成失败，请检查 Chromium 配置后重试: %w", cause)
	}
	return errors.New("浏览器截图生成失败，请检查 Chromium 配置后重试")
}

func landingScreenshotBrowser(configured string) (string, error) {
	if configured != "" {
		path, err := exec.LookPath(configured)
		if err == nil {
			return path, nil
		}
		return "", errors.New("截图浏览器不存在，请配置 Chromium 可执行文件")
	}
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	if runtime.GOOS == "darwin" {
		path := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", errors.New("未找到截图浏览器，请安装 Chromium")
}

func landingScreenshotRequestAllowed(raw, method, origin, documentPath, assetPath string) bool {
	if method != http.MethodGet && method != http.MethodHead {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme+"://"+u.Host != origin || u.Fragment != "" {
		return false
	}
	if u.Path == documentPath {
		return u.RawQuery == ""
	}
	return u.Path == assetPath && u.RawQuery != ""
}

func screenshotBrowserEnvironment() []string {
	var env []string
	for _, key := range []string{"PATH", "TMPDIR", "LANG", "LC_ALL", "SYSTEMROOT"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}

// Chromium's remote-debugging pipe uses fd3/fd4 and NUL-delimited JSON. Keeping
// it on inherited pipes avoids exposing a debugging TCP port to other users.
func startLandingScreenshotBrowser(ctx context.Context, binary, profile string, noSandbox bool) (*screenshotCDP, func(), error) {
	commandRead, commandWrite, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	responseRead, responseWrite, err := os.Pipe()
	if err != nil {
		commandRead.Close()
		commandWrite.Close()
		return nil, nil, err
	}
	args := []string{"--headless=new", "--remote-debugging-pipe", "--user-data-dir=" + profile, "--no-first-run", "--no-default-browser-check", "--disable-background-networking", "--disable-component-update", "--disable-default-apps", "--disable-extensions", "--disable-sync", "--disable-breakpad", "--disable-crash-reporter", "--disable-dev-shm-usage", "--disable-gpu", "--hide-scrollbars", "--mute-audio", "--no-pings", "--dns-prefetch-disable", "--force-webrtc-ip-handling-policy=disable_non_proxied_udp", "--disable-features=MediaRouter,OptimizationHints,AutofillServerCommunication,Translate", "--metrics-recording-only", "--password-store=basic", "--use-mock-keychain", "--window-size=390,844"}
	if noSandbox {
		args = append(args, "--no-sandbox")
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	configureScreenshotProcess(cmd)
	cmd.Env = append(screenshotBrowserEnvironment(), "HOME="+profile, "XDG_CONFIG_HOME="+profile+"/.config", "XDG_CACHE_HOME="+profile+"/.cache")
	cmd.ExtraFiles = []*os.File{commandRead, responseWrite}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		commandRead.Close()
		commandWrite.Close()
		responseRead.Close()
		responseWrite.Close()
		return nil, nil, errors.New("截图浏览器启动失败，请检查 Chromium 配置")
	}
	commandRead.Close()
	responseWrite.Close()
	pipe := newScreenshotCDP(responseRead, commandWrite)
	processDone := make(chan struct{})
	go func() { _ = cmd.Wait(); close(processDone) }()
	stop := func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = pipe.call(closeCtx, "", "Browser.close", map[string]any{}, nil)
		cancel()
		pipe.close()
		// Also collect renderers/utility processes if Chromium could not close.
		killScreenshotProcess(cmd)
		select {
		case <-processDone:
		case <-time.After(time.Second):
			killScreenshotProcess(cmd)
			<-processDone
		}
	}
	return pipe, stop, nil
}

type screenshotCDPMessage struct {
	ID        int64           `json:"id"`
	SessionID string          `json:"sessionId,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}
type screenshotCDP struct {
	reader   io.ReadCloser
	writer   io.WriteCloser
	mu       sync.Mutex
	writeMu  sync.Mutex
	sequence int64
	pending  map[int64]chan screenshotCDPMessage
	events   chan screenshotCDPMessage
	done     chan struct{}
	once     sync.Once
}

func newScreenshotCDP(reader io.ReadCloser, writer io.WriteCloser) *screenshotCDP {
	pipe := &screenshotCDP{reader: reader, writer: writer, pending: map[int64]chan screenshotCDPMessage{}, events: make(chan screenshotCDPMessage, 256), done: make(chan struct{})}
	go pipe.read()
	return pipe
}
func (p *screenshotCDP) close() {
	p.once.Do(func() { close(p.done); p.reader.Close(); p.writer.Close() })
}
func (p *screenshotCDP) read() {
	defer p.close()
	reader := bufio.NewReaderSize(p.reader, 64<<10)
	for {
		var data []byte
		for {
			part, err := reader.ReadSlice(0)
			if len(data)+len(part) > 8<<20 {
				return
			}
			data = append(data, part...)
			if err == bufio.ErrBufferFull {
				continue
			}
			if err != nil {
				return
			}
			break
		}
		var message screenshotCDPMessage
		if json.Unmarshal(bytes.TrimSuffix(data, []byte{0}), &message) != nil {
			return
		}
		if message.ID != 0 {
			p.mu.Lock()
			reply := p.pending[message.ID]
			delete(p.pending, message.ID)
			p.mu.Unlock()
			if reply != nil {
				reply <- message
			}
			continue
		}
		select {
		case p.events <- message:
		default:
			return
		}
	}
}
func (p *screenshotCDP) call(ctx context.Context, session, method string, params any, result any) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return io.EOF
	default:
	}
	p.mu.Lock()
	p.sequence++
	id := p.sequence
	reply := make(chan screenshotCDPMessage, 1)
	p.pending[id] = reply
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.pending, id); p.mu.Unlock() }()
	message := map[string]any{"id": id, "method": method, "params": params}
	if session != "" {
		message["sessionId"] = session
	}
	raw, err := json.Marshal(message)
	if err != nil {
		return err
	}
	raw = append(raw, 0)
	p.writeMu.Lock()
	_, err = p.writer.Write(raw)
	p.writeMu.Unlock()
	if err != nil {
		return err
	}
	select {
	case response := <-reply:
		if response.Error != nil {
			return fmt.Errorf("browser protocol %s failed", method)
		}
		if result != nil {
			return json.Unmarshal(response.Result, result)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-p.done:
		return io.EOF
	}
}
