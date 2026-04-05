package main
//http://localhost:8080
import (
	"net/http"

	"youtube-stats/websocket"
)

func homePage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func stats(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Upgrade(w, r)
	if err != nil {
		return
	}
	go websocket.Writer(ws)
}

func setupRoutes() {
	http.HandleFunc("/", homePage)
	http.HandleFunc("/stats", stats)
	http.ListenAndServe(":8080", nil)
}

func main() {
	setupRoutes()
}
