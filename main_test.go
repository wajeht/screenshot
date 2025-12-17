package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("ok"))
	})

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if rec.Body.String() != "ok" {
		t.Errorf("expected body %q, got %q", "ok", rec.Body.String())
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "text/plain; charset=utf-8" {
		t.Errorf("expected content-type %q, got %q", "text/plain; charset=utf-8", contentType)
	}
}

func TestBasicAuth(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatalf("failed to parse templates: %v", err)
	}

	tests := []struct {
		name           string
		path           string
		password       string
		configPassword string
		expectedStatus int
	}{
		{
			name:           "screenshots without auth when auth configured",
			path:           "/screenshots",
			password:       "",
			configPassword: "secret",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "screenshots with wrong password",
			path:           "/screenshots",
			password:       "wrong",
			configPassword: "secret",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "screenshots with correct password",
			path:           "/screenshots",
			password:       "secret",
			configPassword: "secret",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "domains.json without auth when auth configured",
			path:           "/domains.json",
			password:       "",
			configPassword: "secret",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "domains.json with correct password",
			path:           "/domains.json",
			password:       "secret",
			configPassword: "secret",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "screenshots accessible when no auth configured",
			path:           "/screenshots",
			password:       "",
			configPassword: "",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, err := NewScreenshotRepository(":memory:?cache=shared")
			if err != nil {
				t.Fatalf("failed to create repo: %v", err)
			}
			defer repo.Close()

			s := &Server{
				config: Config{
					Password: tt.configPassword,
				},
				templates: templates,
				repo:      repo,
			}

			var handler http.HandlerFunc
			switch tt.path {
			case "/screenshots":
				handler = s.basicAuth(s.handleScreenshots)
			case "/domains.json":
				handler = s.basicAuth(s.handleDomains)
			}

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.password != "" {
				req.SetBasicAuth("", tt.password)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			if tt.expectedStatus == http.StatusUnauthorized {
				wwwAuth := rec.Header().Get("WWW-Authenticate")
				if wwwAuth == "" {
					t.Error("expected WWW-Authenticate header to be set")
				}
			}
		})
	}
}
