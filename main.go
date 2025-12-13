package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"

	"github.com/wajeht/screenshot/assets"
)

type Config struct {
	Port            string
	PageTimeout     time.Duration
	ScreenshotQual  int
	CacheTTLSecs    int
	MaxWidth        int
	MaxHeight       int
	MaxConcurrent   int
	ShutdownTimeout time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	MinUserAgentLen int
	Debug           bool
	BlockFonts      bool
	BlockMedia      bool
}

func DefaultConfig() Config {
	return Config{
		Port:            ":80",
		PageTimeout:     30 * time.Second,
		ScreenshotQual:  50,
		CacheTTLSecs:    300,
		MaxWidth:        1920,
		MaxHeight:       1920,
		MaxConcurrent:   10,
		ShutdownTimeout: 30 * time.Second,
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    60 * time.Second,
		IdleTimeout:     120 * time.Second,
		MinUserAgentLen: 20,
		Debug:           true,
		BlockFonts:      true,
		BlockMedia:      true,
	}
}

type Dimension struct {
	Width  int
	Height int
}

var presets = map[string]Dimension{
	"og":      {1200, 630},
	"twitter": {1200, 675},
	"square":  {1080, 1080},
	"mobile":  {375, 667},
	"desktop": {1920, 1080},
}

var blockedBots = []string{
	"bot", "crawler", "spider", "crawling",
	"googlebot", "bingbot", "yandex", "baidu", "duckduckbot",
	"slurp", "ia_archiver", "facebookexternalhit", "twitterbot",
	"linkedinbot", "embedly", "quora", "pinterest", "slackbot",
	"discordbot", "telegrambot", "whatsapp", "applebot",
	"semrush", "ahref", "mj12bot", "dotbot", "petalbot",
	"curl", "wget", "python", "httpie", "postman", "insomnia",
	"java", "ruby", "perl", "php", "go-http-client",
	"scrapy", "httpclient", "apache-http", "okhttp",
}

var criticalDomains = []string{
	"google-analytics.com", "googletagmanager.com", "hotjar.com",
	"mixpanel.com", "segment.io", "newrelic.com", "nr-data.net", "sentry.io",
	"doubleclick.net", "googlesyndication.com", "adservice.google.com",
	"facebook.net", "ads.linkedin.com", "accounts.google.com",
	"platform.linkedin.com", "connect.facebook.net", "ponf.linkedin.com",
	"px.ads.linkedin.com", "bat.bing.com", "tr.snapchat.com",
	"li.protechts.net", "challenges.cloudflare.com",
	"intercom.io", "crisp.chat", "drift.com", "zendesk.com",
}

type Blocklist struct {
	domains map[string]struct{}
	mu      sync.RWMutex
	logger  *slog.Logger
}

func NewBlocklist(logger *slog.Logger) (*Blocklist, error) {
	bl := &Blocklist{
		domains: make(map[string]struct{}),
		logger:  logger,
	}

	for _, d := range criticalDomains {
		bl.domains[d] = struct{}{}
	}

	data, err := assets.EmbeddedFiles.ReadFile("filters/domains.json")
	if err != nil {
		return nil, fmt.Errorf("reading domains.json: %w", err)
	}

	var domainList []string
	if err := json.Unmarshal(data, &domainList); err != nil {
		return nil, fmt.Errorf("parsing domains.json: %w", err)
	}

	for _, d := range domainList {
		bl.domains[d] = struct{}{}
	}

	logger.Info("blocklist loaded", slog.Int("domains", len(bl.domains)))
	return bl, nil
}

func isIPAddress(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil {
			return false
		}
	}
	return true
}

func (bl *Blocklist) IsBlocked(url string) bool {
	bl.mu.RLock()
	defer bl.mu.RUnlock()

	url = strings.ToLower(url)

	if idx := strings.Index(url, "://"); idx != -1 {
		url = url[idx+3:]
	}
	if idx := strings.Index(url, "/"); idx != -1 {
		url = url[:idx]
	}
	if idx := strings.Index(url, ":"); idx != -1 {
		url = url[:idx]
	}

	host := url

	if _, ok := bl.domains[host]; ok {
		return true
	}

	parts := strings.Split(host, ".")
	for i := 1; i < len(parts)-1; i++ {
		parent := strings.Join(parts[i:], ".")
		if _, ok := bl.domains[parent]; ok {
			return true
		}
	}

	return false
}

type Server struct {
	browser   *rod.Browser
	semaphore chan struct{}
	config    Config
	logger    *slog.Logger
	blocklist *Blocklist
}

func NewServer(cfg Config, logger *slog.Logger) (*Server, error) {
	blocklist, err := NewBlocklist(logger)
	if err != nil {
		logger.Warn("failed to initialize blocklist", slog.String("error", err.Error()))
		blocklist = &Blocklist{domains: make(map[string]struct{}), logger: logger}
	}

	path, found := launcher.LookPath()
	if !found {
		return nil, fmt.Errorf("browser not found")
	}

	launchURL := launcher.New().
		Bin(path).
		Headless(true).
		Set("no-sandbox").
		Set("disable-gpu").
		Set("disable-dev-shm-usage").
		Set("disable-extensions").
		Set("disable-plugins").
		Set("disable-background-networking").
		Set("disable-background-timer-throttling").
		Set("disable-backgrounding-occluded-windows").
		Set("disable-renderer-backgrounding").
		Set("disable-sync").
		Set("disable-translate").
		Set("disable-default-apps").
		Set("no-first-run").
		Set("hide-scrollbars").
		Set("mute-audio").
		MustLaunch()

	browser := rod.New().ControlURL(launchURL)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("connecting to browser: %w", err)
	}

	return &Server{
		browser:   browser,
		semaphore: make(chan struct{}, cfg.MaxConcurrent),
		config:    cfg,
		logger:    logger,
		blocklist: blocklist,
	}, nil
}

func (s *Server) Close() error {
	return s.browser.Close()
}

func (s *Server) isBot(userAgent string) bool {
	if len(userAgent) < s.config.MinUserAgentLen {
		return true
	}

	ua := strings.ToLower(userAgent)
	for _, bot := range blockedBots {
		if strings.Contains(ua, bot) {
			return true
		}
	}
	return false
}

func getETag(url string) string {
	hash := md5.Sum([]byte(url + time.Now().Format("2006-01-02-15")))
	return hex.EncodeToString(hash[:])
}

func parseIntParam(r *http.Request, name string, defaultVal, maxVal int) int {
	val := r.URL.Query().Get(name)
	if val == "" {
		return defaultVal
	}

	n, err := strconv.Atoi(val)
	if err != nil || n <= 0 {
		return defaultVal
	}
	if n > maxVal {
		return maxVal
	}
	return n
}

func (s *Server) HandleRobots(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
}

func (s *Server) HandleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) HandleFavicon(w http.ResponseWriter, _ *http.Request) {
	data, err := assets.EmbeddedFiles.ReadFile("static/favicon.ico")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/x-icon")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(data)
}

func (s *Server) HandleWebManifest(w http.ResponseWriter, _ *http.Request) {
	data, err := assets.EmbeddedFiles.ReadFile("static/site.webmanifest")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(data)
}

func (s *Server) HandleScreenshot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userAgent := r.Header.Get("User-Agent")
	if s.isBot(userAgent) {
		s.logger.Warn("blocked bot request", slog.String("ua", userAgent), slog.String("ip", r.RemoteAddr))
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		http.Error(w, "missing /?url=<url>", http.StatusBadRequest)
		return
	}

	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		targetURL = "https://" + targetURL
	}

	dim := presets["og"]
	if preset := r.URL.Query().Get("preset"); preset != "" {
		if p, ok := presets[preset]; ok {
			dim = p
		}
	}
	width, height := dim.Width, dim.Height

	if r.URL.Query().Get("width") != "" {
		width = parseIntParam(r, "width", width, s.config.MaxWidth)
	}
	if r.URL.Query().Get("height") != "" {
		height = parseIntParam(r, "height", height, s.config.MaxHeight)
	}

	fullPage := r.URL.Query().Get("full") == "true"

	etag := getETag(targetURL)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	select {
	case s.semaphore <- struct{}{}:
		defer func() { <-s.semaphore }()
	case <-ctx.Done():
		http.Error(w, "request cancelled", http.StatusServiceUnavailable)
		return
	}

	screenshot, timing, err := s.captureScreenshot(targetURL, width, height, fullPage)
	if err != nil {
		s.handleCaptureError(w, targetURL, err, timing)
		return
	}

	s.logger.Info("screenshot captured",
		slog.String("url", targetURL),
		slog.Int64("setup_ms", timing.Setup.Milliseconds()),
		slog.Int64("nav_ms", timing.Navigation.Milliseconds()),
		slog.Int64("load_ms", timing.Load.Milliseconds()),
		slog.Int64("screenshot_ms", timing.Screenshot.Milliseconds()),
		slog.Int64("total_ms", timing.Total.Milliseconds()),
		slog.Int("size_kb", len(screenshot)/1024),
	)

	s.writeScreenshotResponse(w, screenshot, etag, timing)
}

type Timing struct {
	Setup      time.Duration
	Navigation time.Duration
	Load       time.Duration
	Screenshot time.Duration
	Total      time.Duration
}

func (s *Server) captureScreenshot(url string, width, height int, fullPage bool) ([]byte, Timing, error) {
	var timing Timing
	totalStart := time.Now()

	setupStart := time.Now()
	page, err := s.browser.Page(proto.TargetCreateTarget{})
	if err != nil {
		return nil, timing, fmt.Errorf("creating page: %w", err)
	}
	defer func() { _ = page.Close() }()

	if err := page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:             width,
		Height:            height,
		DeviceScaleFactor: 1.0,
	}); err != nil {
		return nil, timing, fmt.Errorf("setting viewport: %w", err)
	}

	router := page.HijackRequests()
	router.MustAdd("*", func(h *rod.Hijack) {
		reqURL := h.Request.URL().String()
		reqType := h.Request.Type()

		if s.config.BlockFonts && reqType == proto.NetworkResourceTypeFont {
			if s.config.Debug {
				s.logger.Debug("blocked font", slog.String("url", reqURL))
			}
			h.Response.Fail(proto.NetworkErrorReasonBlockedByClient)
			return
		}

		if s.config.BlockMedia {
			if reqType == proto.NetworkResourceTypeMedia ||
				reqType == proto.NetworkResourceTypeWebSocket ||
				strings.HasSuffix(reqURL, ".mp4") ||
				strings.HasSuffix(reqURL, ".webm") ||
				strings.HasSuffix(reqURL, ".mp3") ||
				strings.HasSuffix(reqURL, ".wav") ||
				strings.HasSuffix(reqURL, ".ogg") {
				if s.config.Debug {
					s.logger.Debug("blocked media", slog.String("type", string(reqType)), slog.String("url", reqURL))
				}
				h.Response.Fail(proto.NetworkErrorReasonBlockedByClient)
				return
			}
		}

		switch reqType {
		case proto.NetworkResourceTypeXHR,
			proto.NetworkResourceTypeFetch,
			proto.NetworkResourceTypePing,
			proto.NetworkResourceTypePrefetch,
			proto.NetworkResourceTypeSignedExchange,
			proto.NetworkResourceTypeEventSource,
			proto.NetworkResourceTypeManifest:
			if s.config.Debug {
				s.logger.Debug("blocked unnecessary", slog.String("type", string(reqType)), slog.String("url", reqURL))
			}
			h.Response.Fail(proto.NetworkErrorReasonBlockedByClient)
			return
		}

		if strings.HasSuffix(reqURL, "favicon.ico") ||
			strings.HasSuffix(reqURL, ".webmanifest") ||
			strings.HasSuffix(reqURL, "manifest.json") ||
			strings.Contains(reqURL, "apple-touch-icon") ||
			strings.Contains(reqURL, "android-chrome") {
			if s.config.Debug {
				s.logger.Debug("blocked favicon/manifest", slog.String("url", reqURL))
			}
			h.Response.Fail(proto.NetworkErrorReasonBlockedByClient)
			return
		}

		if s.blocklist.IsBlocked(reqURL) &&
			reqType != proto.NetworkResourceTypeStylesheet &&
			reqType != proto.NetworkResourceTypeDocument {
			if s.config.Debug {
				s.logger.Debug("blocked by blocklist", slog.String("url", reqURL))
			}
			h.Response.Fail(proto.NetworkErrorReasonBlockedByClient)
			return
		}

		if s.config.Debug {
			s.logger.Debug("fetching", slog.String("type", string(reqType)), slog.String("url", reqURL))
		}

		h.ContinueRequest(&proto.FetchContinueRequest{})
	})

	go router.Run()
	defer router.MustStop()
	timing.Setup = time.Since(setupStart)

	navStart := time.Now()
	if err := page.Timeout(s.config.PageTimeout).Navigate(url); err != nil {
		timing.Navigation = time.Since(navStart)
		return nil, timing, fmt.Errorf("navigation timeout: %w", err)
	}
	timing.Navigation = time.Since(navStart)

	loadStart := time.Now()
	if err := page.Timeout(s.config.PageTimeout).WaitLoad(); err != nil {
		timing.Load = time.Since(loadStart)
		return nil, timing, fmt.Errorf("load timeout: %w", err)
	}
	timing.Load = time.Since(loadStart)

	screenshotStart := time.Now()
	quality := s.config.ScreenshotQual
	screenshot, err := page.Screenshot(fullPage, &proto.PageCaptureScreenshot{
		Format:           proto.PageCaptureScreenshotFormatJpeg,
		Quality:          &quality,
		OptimizeForSpeed: true,
	})
	timing.Screenshot = time.Since(screenshotStart)
	timing.Total = time.Since(totalStart)

	if err != nil {
		return nil, timing, fmt.Errorf("capturing screenshot: %w", err)
	}

	return screenshot, timing, nil
}

func (s *Server) handleCaptureError(w http.ResponseWriter, url string, err error, timing Timing) {
	s.logger.Error("screenshot failed",
		slog.String("url", url),
		slog.String("error", err.Error()),
		slog.Int64("elapsed_ms", timing.Total.Milliseconds()),
	)

	if strings.Contains(err.Error(), "timeout") {
		http.Error(w, "timeout loading page", http.StatusGatewayTimeout)
		return
	}
	http.Error(w, "failed to capture screenshot", http.StatusInternalServerError)
}

func (s *Server) writeScreenshotResponse(w http.ResponseWriter, screenshot []byte, etag string, timing Timing) {
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", s.config.CacheTTLSecs))
	w.Header().Set("ETag", etag)
	w.Header().Set("X-Setup-Ms", strconv.FormatInt(timing.Setup.Milliseconds(), 10))
	w.Header().Set("X-Nav-Ms", strconv.FormatInt(timing.Navigation.Milliseconds(), 10))
	w.Header().Set("X-Load-Ms", strconv.FormatInt(timing.Load.Milliseconds(), 10))
	w.Header().Set("X-Screenshot-Ms", strconv.FormatInt(timing.Screenshot.Milliseconds(), 10))
	w.Header().Set("X-Total-Ms", strconv.FormatInt(timing.Total.Milliseconds(), 10))

	if _, err := w.Write(screenshot); err != nil {
		s.logger.Error("failed to write response", slog.String("error", err.Error()))
	}
}

func main() {
	cfg := DefaultConfig()

	logLevel := slog.LevelInfo
	if cfg.Debug {
		logLevel = slog.LevelDebug
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))

	srv, err := NewServer(cfg, logger)
	if err != nil {
		logger.Error("failed to create server", slog.String("error", err.Error()))
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /robots.txt", srv.HandleRobots)
	mux.HandleFunc("GET /healthz", srv.HandleHealth)
	mux.HandleFunc("GET /favicon.ico", srv.HandleFavicon)
	mux.HandleFunc("GET /site.webmanifest", srv.HandleWebManifest)
	mux.Handle("GET /static/", http.FileServer(http.FS(assets.EmbeddedFiles)))
	mux.HandleFunc("GET /", srv.HandleScreenshot)

	httpServer := &http.Server{
		Addr:         cfg.Port,
		Handler:      mux,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	go func() {
		logger.Info("server starting", slog.String("addr", cfg.Port))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigChan

	logger.Info("shutting down", slog.String("signal", sig.String()))

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", slog.String("error", err.Error()))
	}

	if err := srv.Close(); err != nil {
		logger.Error("browser close error", slog.String("error", err.Error()))
	}

	logger.Info("server stopped")
}
