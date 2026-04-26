package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"youtube-stats/board"
	"youtube-stats/websocket"
)

const defaultServerAddr = ":8080"

const dashboardPath = "/dashboard"

func dashboardPage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func rootPage(w http.ResponseWriter, r *http.Request) {
	if slug := defaultPublicBoardSlug(); slug != "" {
		target := "/board/" + url.PathEscape(slug)
		if rawQuery := strings.TrimSpace(r.URL.RawQuery); rawQuery != "" {
			target += "?" + rawQuery
		}
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
		return
	}

	dashboardPage(w, r)
}

func termsPage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "terms.html")
}

func privacyPage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "privacy.html")
}

func stats(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Upgrade(w, r)
	if err != nil {
		return
	}
	go websocket.Writer(ws)
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

func setupRoutes(mux *http.ServeMux) {
	mux.HandleFunc(dashboardPath, dashboardPage)
	mux.HandleFunc("/terms", termsPage)
	mux.HandleFunc("/privacy", privacyPage)
	mux.HandleFunc("/board/", boardPage)
	mux.HandleFunc("/stats", stats)
	mux.HandleFunc("/healthz", healthz)
	mux.HandleFunc("/auth/youtube/start", youtubeAuthStart)
	mux.HandleFunc("/auth/youtube/callback", youtubeAuthCallback)
	mux.HandleFunc("/auth/youtube/disconnect", youtubeDisconnect)
	mux.HandleFunc("/api/youtube/status", youtubeStatus)
	mux.HandleFunc("/api/youtube/stats", youtubeStats)
	mux.HandleFunc("/auth/instagram/start", instagramAuthStart)
	mux.HandleFunc("/auth/instagram/callback", instagramAuthCallback)
	mux.HandleFunc("/auth/instagram/disconnect", instagramDisconnect)
	mux.HandleFunc("/api/instagram/status", instagramStatus)
	mux.HandleFunc("/api/instagram/stats", instagramStats)
	mux.HandleFunc("/auth/tiktok/start", tiktokAuthStart)
	mux.HandleFunc("/auth/tiktok/callback", tiktokAuthCallback)
	mux.HandleFunc("/auth/tiktok/disconnect", tiktokDisconnect)
	mux.HandleFunc("/api/tiktok/status", tiktokStatus)
	mux.HandleFunc("/api/tiktok/stats", tiktokStats)
	mux.HandleFunc("/api/boards/me", myBoard)
	mux.HandleFunc("/api/boards/publish", publishBoard)
	mux.HandleFunc("/api/boards/", publicBoardData)
	mux.HandleFunc("/", rootPage)
}

func main() {
	mux := http.NewServeMux()
	setupRoutes(mux)

	server := &http.Server{
		Addr:              serverAddr(),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}

func serverAddr() string {
	if addr := strings.TrimSpace(os.Getenv("APP_ADDR")); addr != "" {
		return addr
	}
	if port := strings.TrimSpace(os.Getenv("PORT")); port != "" {
		if strings.HasPrefix(port, ":") {
			return port
		}
		return ":" + port
	}

	return defaultServerAddr
}

func defaultPublicBoardSlug() string {
	if slug := strings.TrimSpace(os.Getenv("PUBLIC_ROOT_BOARD_SLUG")); slug != "" {
		return slug
	}

	return board.DefaultRootSlug()
}
