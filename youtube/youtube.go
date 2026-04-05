package youtube

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type Response struct {
	Items []Item    `json:"items"`
	Error *APIError `json:"error,omitempty"`
}

type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Item struct {
	Statistics Statistics `json:"statistics"`
}

type Statistics struct {
	SubscriberCount string `json:"subscriberCount"`
}

func GetSubscribers() (Statistics, error) {
	key := os.Getenv("YOUTUBE_KEY")
	channelID := os.Getenv("CHANNEL_ID")

	if key == "" {
		return Statistics{}, fmt.Errorf("YOUTUBE_KEY is empty")
	}
	if channelID == "" {
		return Statistics{}, fmt.Errorf("CHANNEL_ID is empty")
	}

	req, err := http.NewRequest("GET", "https://www.googleapis.com/youtube/v3/channels", nil)
	if err != nil {
		return Statistics{}, err
	}

	q := req.URL.Query()
	q.Add("key", key)
	q.Add("id", channelID)
	q.Add("part", "statistics")
	req.URL.RawQuery = q.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Statistics{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Statistics{}, err
	}

	var response Response
	if err := json.Unmarshal(body, &response); err != nil {
		return Statistics{}, fmt.Errorf("json parse failed: %w; body=%s", err, string(body))
	}

	if response.Error != nil {
		return Statistics{}, fmt.Errorf("youtube api error %d: %s", response.Error.Code, response.Error.Message)
	}

	if len(response.Items) == 0 {
		return Statistics{}, fmt.Errorf("no channel found for CHANNEL_ID=%q", channelID)
	}

	return response.Items[0].Statistics, nil
}
