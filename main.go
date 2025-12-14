package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"html/template"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pressly/goose/v3"

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

type Dimension struct {
	Width  int
	Height int
}

type Timing struct {
	Setup      time.Duration
	Navigation time.Duration
	Load       time.Duration
	Screenshot time.Duration
	Total      time.Duration
}

type Blocklist struct {
	domains map[string]struct{}
	mu      sync.RWMutex
	logger  *slog.Logger
}

type Server struct {
	browser   *rod.Browser
	semaphore chan struct{}
	config    Config
	logger    *slog.Logger
	blocklist *Blocklist
	templates map[string]*template.Template
	repo      *ScreenshotRepository
}

type PageData struct {
	Title   string
	Code    int
	Message string
}

type ScreenshotRepository struct {
	db *sql.DB
}

const (
	maxOpenConns    = 100
	maxIdleDBConns  = 25
	connMaxLifetime = 5 * time.Minute
)

var ErrNotFound = errors.New("screenshot not found")

func NewScreenshotRepository(dbPath string) (*ScreenshotRepository, error) {
	path := strings.Split(dbPath, "?")[0]
	dir := filepath.Dir(path)

	dbExists := false
	if _, err := os.Stat(path); err == nil {
		dbExists = true
	}

	if !dbExists {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create database directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleDBConns)
	db.SetConnMaxLifetime(connMaxLifetime)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	if err := applyPragmas(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to apply pragmas: %w", err)
	}

	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return &ScreenshotRepository{db: db}, nil
}

func applyPragmas(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=10000",
		"PRAGMA temp_store=MEMORY",
		"PRAGMA mmap_size=268435456",
	}

	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			log.Printf("Warning: Failed to set pragma %s: %v", pragma, err)
		}
	}

	return nil
}

func runMigrations(db *sql.DB) error {
	goose.SetBaseFS(assets.EmbeddedFiles)

	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

func (r *ScreenshotRepository) Get(url string, width, height int) ([]byte, string, error) {
	var data []byte
	var contentType string

	query := `SELECT data, content_type FROM screenshots WHERE url = ? AND width = ? AND height = ?`
	err := r.db.QueryRow(query, url, width, height).Scan(&data, &contentType)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", ErrNotFound
		}
		return nil, "", fmt.Errorf("failed to get screenshot: %w", err)
	}

	return data, contentType, nil
}

func (r *ScreenshotRepository) Save(url string, data []byte, contentType string, width, height int) error {
	query := `INSERT OR REPLACE INTO screenshots (url, data, content_type, width, height) VALUES (?, ?, ?, ?, ?)`
	_, err := r.db.Exec(query, url, data, contentType, width, height)
	if err != nil {
		return fmt.Errorf("failed to save screenshot: %w", err)
	}
	return nil
}

func (r *ScreenshotRepository) List() (string, error) {
	query := `
		SELECT json_group_array(
			json_object(
				'id', id,
				'url', url,
				'data_size', length(data),
				'content_type', content_type,
				'width', width,
				'height', height,
				'created_at', created_at
			)
		)
		FROM screenshots
		ORDER BY id DESC
	`

	rows, err := r.db.Query(query)
	if err != nil {
		return "", fmt.Errorf("failed to list screenshots: %w", err)
	}
	defer rows.Close()

	var jsonResult string
	if rows.Next() {
		if err := rows.Scan(&jsonResult); err != nil {
			return "", fmt.Errorf("failed to scan result: %w", err)
		}
	}

	return jsonResult, nil
}

func (r *ScreenshotRepository) Ping() error {
	return r.db.Ping()
}

func (r *ScreenshotRepository) Close() error {
	return r.db.Close()
}

var (
	presets = map[string]Dimension{
		"thumb":   {800, 420},
		"og":      {1200, 630},
		"twitter": {1200, 675},
		"square":  {1080, 1080},
		"mobile":  {375, 667},
		"desktop": {1920, 1080},
	}

	botPattern = regexp.MustCompile(`(?i)bot|crawler|spider|crawling|googlebot|bingbot|yandex|baidu|duckduckbot|slurp|ia_archiver|facebookexternalhit|twitterbot|linkedinbot|embedly|quora|pinterest|slackbot|discordbot|telegrambot|whatsapp|applebot|semrush|ahref|mj12bot|dotbot|petalbot|curl|wget|python|httpie|postman|insomnia|java|ruby|perl|php|go-http-client|scrapy|httpclient|apache-http|okhttp`)

	blockedExtensions = map[string]struct{}{
		".mp4": {}, ".webm": {}, ".mp3": {}, ".wav": {}, ".ogg": {},
		".ico": {}, ".webmanifest": {},
	}

	blockedPaths = []string{"apple-touch-icon", "android-chrome", "manifest.json"}

	criticalDomains = []string{
		"google-analytics.com", "googletagmanager.com", "hotjar.com",
		"mixpanel.com", "segment.io", "newrelic.com", "nr-data.net", "sentry.io",
		"doubleclick.net", "googlesyndication.com", "adservice.google.com",
		"facebook.net", "ads.linkedin.com", "accounts.google.com",
		"platform.linkedin.com", "connect.facebook.net", "ponf.linkedin.com",
		"px.ads.linkedin.com", "bat.bing.com", "tr.snapchat.com",
		"li.protechts.net", "challenges.cloudflare.com",
		"intercom.io", "crisp.chat", "drift.com", "zendesk.com",
	}
)

func DefaultConfig() Config {
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "80"
	}

	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "development"
	}

	debug := env != "production"

	return Config{
		Port:            ":" + port,
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
		Debug:           debug,
		BlockFonts:      true,
		BlockMedia:      true,
	}
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

func (bl *Blocklist) IsBlocked(host string) bool {
	bl.mu.RLock()
	defer bl.mu.RUnlock()

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

func NewServer(cfg Config, logger *slog.Logger, repo *ScreenshotRepository) (*Server, error) {
	blocklist, err := NewBlocklist(logger)
	if err != nil {
		logger.Warn("failed to initialize blocklist", slog.String("error", err.Error()))
		blocklist = &Blocklist{domains: make(map[string]struct{}), logger: logger}
	}

	templates, err := parseTemplates()
	if err != nil {
		return nil, fmt.Errorf("parsing templates: %w", err)
	}

	path, found := launcher.LookPath()
	if !found {
		return nil, errors.New("browser not found")
	}

	url := launcher.New().
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

	browser := rod.New().ControlURL(url)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("connecting to browser: %w", err)
	}

	return &Server{
		browser:   browser,
		semaphore: make(chan struct{}, cfg.MaxConcurrent),
		config:    cfg,
		logger:    logger,
		blocklist: blocklist,
		templates: templates,
		repo:      repo,
	}, nil
}

func parseTemplates() (map[string]*template.Template, error) {
	templates := make(map[string]*template.Template)
	pages := []string{"index", "404", "500", "error"}

	base, err := assets.EmbeddedFiles.ReadFile("templates/base.html")
	if err != nil {
		return nil, err
	}

	for _, page := range pages {
		content, err := assets.EmbeddedFiles.ReadFile("templates/" + page + ".html")
		if err != nil {
			return nil, err
		}

		tmpl, err := template.New("base").Parse(string(base))
		if err != nil {
			return nil, err
		}

		tmpl, err = tmpl.Parse(string(content))
		if err != nil {
			return nil, err
		}

		templates[page] = tmpl
	}

	return templates, nil
}

func (s *Server) Close() error {
	if s.repo != nil {
		s.repo.Close()
	}
	return s.browser.Close()
}

func (s *Server) ServeHTTP(mux *http.ServeMux) {
	mux.Handle("GET /static/", http.FileServer(http.FS(assets.EmbeddedFiles)))
	mux.HandleFunc("GET /robots.txt", s.handleRobots)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /favicon.ico", s.handleFavicon)
	mux.HandleFunc("GET /site.webmanifest", s.handleWebManifest)
	mux.HandleFunc("GET /blocked", s.handleBlocked)
	mux.HandleFunc("GET /domains.json", s.handleDomains)
	mux.HandleFunc("GET /screenshots", s.handleScreenshots)
	mux.HandleFunc("GET /{$}", s.handleScreenshot)
	mux.HandleFunc("/", s.handleNotFound)
}

func (s *Server) handleNotFound(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	s.templates["404"].Execute(w, PageData{Title: "404 - Not Found"})
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.templates["index"].Execute(w, PageData{Title: "Screenshot"})
}

func (s *Server) handleError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	s.templates["error"].Execute(w, PageData{
		Title:   fmt.Sprintf("%d - Error", code),
		Code:    code,
		Message: message,
	})
}

func (s *Server) handleRobots(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("User-agent: *\nDisallow: /\n"))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	if s.repo != nil {
		if err := s.repo.Ping(); err != nil {
			http.Error(w, "database connection failed", http.StatusServiceUnavailable)
			return
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok"))
}

func (s *Server) handleFavicon(w http.ResponseWriter, _ *http.Request) {
	data, err := assets.EmbeddedFiles.ReadFile("static/favicon.ico")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/x-icon")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

func (s *Server) handleWebManifest(w http.ResponseWriter, _ *http.Request) {
	data, err := assets.EmbeddedFiles.ReadFile("static/site.webmanifest")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

func (s *Server) handleBlocked(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		http.Error(w, "missing domain parameter", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if s.blocklist.IsBlocked(domain) {
		w.Write([]byte("blocked"))
	} else {
		w.Write([]byte("allowed"))
	}
}

func (s *Server) handleDomains(w http.ResponseWriter, _ *http.Request) {
	data, err := assets.EmbeddedFiles.ReadFile("filters/domains.json")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

func (s *Server) handleScreenshots(w http.ResponseWriter, _ *http.Request) {
	if s.repo == nil {
		http.Error(w, "database not configured", http.StatusServiceUnavailable)
		return
	}

	jsonResult, err := s.repo.List()
	if err != nil {
		s.logger.Error("failed to list screenshots", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.Write([]byte(jsonResult))
}

func (s *Server) handleScreenshot(w http.ResponseWriter, r *http.Request) {
	userAgent := r.Header.Get("User-Agent")
	if s.isBot(userAgent) {
		s.logger.Warn("blocked bot request", slog.String("ua", userAgent), slog.String("ip", r.RemoteAddr))
		s.handleError(w, http.StatusForbidden, "Forbidden")
		return
	}

	targetURL := r.URL.Query().Get("url")
	if targetURL == "" {
		s.handleIndex(w, r)
		return
	}

	if !strings.HasPrefix(targetURL, "http://") && !strings.HasPrefix(targetURL, "https://") {
		targetURL = "https://" + targetURL
	}

	width, height := s.parseDimensions(r)
	fullPage := r.URL.Query().Get("full") == "true"

	etag := generateETag(targetURL, width, height)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	if s.repo != nil && !fullPage {
		if data, contentType, err := s.repo.Get(targetURL, width, height); err == nil {
			s.logger.Info("screenshot served from cache",
				slog.String("url", targetURL),
				slog.Int("width", width),
				slog.Int("height", height),
			)
			s.writeCachedResponse(w, data, contentType, etag)
			return
		}
	}

	select {
	case s.semaphore <- struct{}{}:
		defer func() { <-s.semaphore }()
	case <-r.Context().Done():
		s.handleError(w, http.StatusServiceUnavailable, "Request cancelled")
		return
	}

	screenshot, timing, err := s.capture(targetURL, width, height, fullPage)
	if err != nil {
		s.handleCaptureError(w, targetURL, err, timing)
		return
	}

	if s.repo != nil && !fullPage {
		if err := s.repo.Save(targetURL, screenshot, "image/webp", width, height); err != nil {
			s.logger.Warn("failed to cache screenshot", slog.String("url", targetURL), slog.String("error", err.Error()))
		}
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

	s.writeResponse(w, screenshot, etag, timing)
}

func (s *Server) parseDimensions(r *http.Request) (width, height int) {
	dim := presets["thumb"]
	if preset := r.URL.Query().Get("preset"); preset != "" {
		if p, ok := presets[preset]; ok {
			dim = p
		}
	}
	width, height = dim.Width, dim.Height

	if r.URL.Query().Get("width") != "" {
		width = parseIntParam(r, "width", width, s.config.MaxWidth)
	}
	if r.URL.Query().Get("height") != "" {
		height = parseIntParam(r, "height", height, s.config.MaxHeight)
	}
	return width, height
}

func (s *Server) capture(url string, width, height int, fullPage bool) ([]byte, Timing, error) {
	var timing Timing
	totalStart := time.Now()

	setupStart := time.Now()
	page, err := s.browser.Page(proto.TargetCreateTarget{})
	if err != nil {
		return nil, timing, fmt.Errorf("creating page: %w", err)
	}
	defer page.Close()

	if err := page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:             width,
		Height:            height,
		DeviceScaleFactor: 1.0,
	}); err != nil {
		return nil, timing, fmt.Errorf("setting viewport: %w", err)
	}

	router := page.HijackRequests()
	router.MustAdd("*", s.createRequestHandler())
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
		Format:           proto.PageCaptureScreenshotFormatWebp,
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

func (s *Server) createRequestHandler() func(*rod.Hijack) {
	return func(h *rod.Hijack) {
		reqURL := h.Request.URL().String()
		reqType := h.Request.Type()

		if s.shouldBlock(reqURL, reqType) {
			h.Response.Fail(proto.NetworkErrorReasonBlockedByClient)
			return
		}

		if s.config.Debug {
			s.logger.Debug("fetching", slog.String("type", string(reqType)), slog.String("url", reqURL))
		}
		h.ContinueRequest(&proto.FetchContinueRequest{})
	}
}

func (s *Server) shouldBlock(reqURL string, reqType proto.NetworkResourceType) bool {
	if s.config.BlockFonts && reqType == proto.NetworkResourceTypeFont {
		if s.config.Debug {
			s.logger.Debug("blocked font", slog.String("url", reqURL))
		}
		return true
	}

	if s.config.BlockMedia {
		if reqType == proto.NetworkResourceTypeMedia || reqType == proto.NetworkResourceTypeWebSocket {
			if s.config.Debug {
				s.logger.Debug("blocked media", slog.String("type", string(reqType)), slog.String("url", reqURL))
			}
			return true
		}
		if idx := strings.LastIndexByte(reqURL, '.'); idx != -1 {
			if _, blocked := blockedExtensions[reqURL[idx:]]; blocked {
				if s.config.Debug {
					s.logger.Debug("blocked media", slog.String("url", reqURL))
				}
				return true
			}
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
		return true
	}

	for _, path := range blockedPaths {
		if strings.Contains(reqURL, path) {
			if s.config.Debug {
				s.logger.Debug("blocked favicon/manifest", slog.String("url", reqURL))
			}
			return true
		}
	}

	if reqType != proto.NetworkResourceTypeStylesheet && reqType != proto.NetworkResourceTypeDocument {
		host := extractHost(reqURL)
		if s.blocklist.IsBlocked(host) {
			if s.config.Debug {
				s.logger.Debug("blocked by blocklist", slog.String("url", reqURL))
			}
			return true
		}
	}

	return false
}

func (s *Server) handleCaptureError(w http.ResponseWriter, url string, err error, timing Timing) {
	s.logger.Error("screenshot failed",
		slog.String("url", url),
		slog.String("error", err.Error()),
		slog.Int64("elapsed_ms", timing.Total.Milliseconds()),
	)

	if strings.Contains(err.Error(), "timeout") {
		s.handleError(w, http.StatusGatewayTimeout, "Timeout loading page")
		return
	}

	s.handleError(w, http.StatusInternalServerError, "Failed to capture screenshot")
}

func (s *Server) writeResponse(w http.ResponseWriter, screenshot []byte, etag string, timing Timing) {
	w.Header().Set("Content-Type", "image/webp")
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

func (s *Server) writeCachedResponse(w http.ResponseWriter, data []byte, contentType, etag string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", s.config.CacheTTLSecs))
	w.Header().Set("ETag", etag)
	w.Header().Set("X-Cache", "HIT")

	if _, err := w.Write(data); err != nil {
		s.logger.Error("failed to write cached response", slog.String("error", err.Error()))
	}
}

func (s *Server) isBot(userAgent string) bool {
	return len(userAgent) < s.config.MinUserAgentLen || botPattern.MatchString(userAgent)
}

func extractHost(rawURL string) string {
	url := rawURL
	if idx := strings.Index(url, "://"); idx != -1 {
		url = url[idx+3:]
	}
	if idx := strings.IndexByte(url, '/'); idx != -1 {
		url = url[:idx]
	}
	if idx := strings.IndexByte(url, ':'); idx != -1 {
		url = url[:idx]
	}
	return strings.ToLower(url)
}

func generateETag(url string, width, height int) string {
	h := fnv.New64a()
	h.Write([]byte(url))
	h.Write([]byte(fmt.Sprintf("%d:%d", width, height)))
	h.Write([]byte(time.Now().Format("2006-01-02-15")))
	return strconv.FormatUint(h.Sum64(), 36)
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

func run() error {
	cfg := DefaultConfig()

	logLevel := slog.LevelInfo
	if cfg.Debug {
		logLevel = slog.LevelDebug
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))

	repo, err := NewScreenshotRepository("./data/db.sqlite?cache=shared&mode=rwc&_journal_mode=WAL")
	if err != nil {
		return fmt.Errorf("creating repository: %w", err)
	}
	defer repo.Close()

	srv, err := NewServer(cfg, logger, repo)
	if err != nil {
		return fmt.Errorf("creating server: %w", err)
	}
	defer srv.Close()

	mux := http.NewServeMux()
	srv.ServeHTTP(mux)

	httpServer := &http.Server{
		Addr:         cfg.Port,
		Handler:      mux,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	errChan := make(chan error, 1)
	go func() {
		logger.Info("server starting", slog.String("addr", cfg.Port))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errChan:
		return fmt.Errorf("server error: %w", err)
	case sig := <-sigChan:
		logger.Info("shutting down", slog.String("signal", sig.String()))
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown error: %w", err)
	}

	logger.Info("server stopped")
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
