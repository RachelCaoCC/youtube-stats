package websocket

import (
	"log"
	"net/http"
	"time"

	gws "github.com/gorilla/websocket"
	"youtube-stats/youtube"
)

var upgrader = gws.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func Upgrade(w http.ResponseWriter, r *http.Request) (*gws.Conn, error) {
	return upgrader.Upgrade(w, r, nil)
}

func Writer(conn *gws.Conn) {
	defer conn.Close()

	for {
		stats, err := youtube.GetSubscribers()
		if err != nil {
			log.Println("GetSubscribers error:", err)

			err = conn.WriteJSON(map[string]string{
				"error": err.Error(),
			})
			if err != nil {
				log.Println("WebSocket write error:", err)
				return
			}

			time.Sleep(5 * time.Second)
			continue
		}

		err = conn.WriteJSON(map[string]string{
			"subscriberCount": stats.SubscriberCount,
		})
		if err != nil {
			log.Println("WebSocket write error:", err)
			return
		}

		time.Sleep(5 * time.Second)
	}
}
