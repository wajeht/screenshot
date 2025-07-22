package main

import (
	"net/http"
	"strings"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

func handleHome(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	if url == "" {
		http.Error(w, "missing /?url=<url>", http.StatusBadRequest)
		return
	}

	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}

	launchURL := launcher.NewUserMode().
		Headless(true).
		Set("no-sandbox").
		MustLaunch()

	browser := rod.New().ControlURL(launchURL).MustConnect()
	defer browser.MustClose()

	page := browser.MustPage(url).MustWaitLoad()
	screenshot := page.MustScreenshot()

	w.Header().Set("Content-Type", "image/png")
	w.Write(screenshot)
}

func main() {
	http.HandleFunc("/", handleHome)
	http.ListenAndServe(":80", nil)
}

