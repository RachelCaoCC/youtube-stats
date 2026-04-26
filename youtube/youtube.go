package youtube

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
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
	ID             string         `json:"id"`
	Snippet        Snippet        `json:"snippet"`
	Statistics     Statistics     `json:"statistics"`
	ContentDetails ContentDetails `json:"contentDetails"`
}

type Snippet struct {
	Title      string       `json:"title"`
	Thumbnails ThumbnailSet `json:"thumbnails"`
}

type ThumbnailSet struct {
	Default  *Thumbnail `json:"default"`
	Medium   *Thumbnail `json:"medium"`
	High     *Thumbnail `json:"high"`
	Standard *Thumbnail `json:"standard"`
	Maxres   *Thumbnail `json:"maxres"`
}

type Thumbnail struct {
	URL string `json:"url"`
}

type ContentDetails struct {
	RelatedPlaylists RelatedPlaylists `json:"relatedPlaylists"`
}

type RelatedPlaylists struct {
	Uploads string `json:"uploads"`
}

type Statistics struct {
	SubscriberCount       string `json:"subscriberCount"`
	ViewCount             string `json:"viewCount"`
	HiddenSubscriberCount bool   `json:"hiddenSubscriberCount"`
}

type Channel struct {
	ID           string
	Title        string
	ThumbnailURL string
	UploadsID    string
	Statistics   Statistics
}

type playlistItemsResponse struct {
	Items         []playlistItem `json:"items"`
	NextPageToken string         `json:"nextPageToken"`
	Error         *APIError      `json:"error,omitempty"`
}

type playlistItem struct {
	Snippet playlistItemSnippet `json:"snippet"`
}

type playlistItemSnippet struct {
	ResourceID playlistItemResourceID `json:"resourceId"`
}

type playlistItemResourceID struct {
	VideoID string `json:"videoId"`
}

type videosResponse struct {
	Items []videoItem `json:"items"`
	Error *APIError   `json:"error,omitempty"`
}

type videoItem struct {
	Statistics videoStatistics `json:"statistics"`
}

type videoStatistics struct {
	LikeCount string `json:"likeCount"`
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

	channel, err := getChannelByID(key, channelID)
	if err != nil {
		return Statistics{}, err
	}

	return channel.Statistics, nil
}

func GetMyChannel(accessToken string) (Channel, error) {
	if strings.TrimSpace(accessToken) == "" {
		return Channel{}, fmt.Errorf("access token is empty")
	}

	req, err := http.NewRequest("GET", "https://www.googleapis.com/youtube/v3/channels", nil)
	if err != nil {
		return Channel{}, err
	}

	q := req.URL.Query()
	q.Add("mine", "true")
	q.Add("part", "snippet,statistics,contentDetails")
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Authorization", "Bearer "+accessToken)

	return doChannelRequest(req)
}

func getChannelByID(key, channelID string) (Channel, error) {
	req, err := http.NewRequest("GET", "https://www.googleapis.com/youtube/v3/channels", nil)
	if err != nil {
		return Channel{}, err
	}

	q := req.URL.Query()
	q.Add("key", key)
	q.Add("id", channelID)
	q.Add("part", "snippet,statistics,contentDetails")
	req.URL.RawQuery = q.Encode()

	return doChannelRequest(req)
}

func doChannelRequest(req *http.Request) (Channel, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Channel{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Channel{}, err
	}

	var response Response
	if err := json.Unmarshal(body, &response); err != nil {
		return Channel{}, fmt.Errorf("json parse failed: %w; body=%s", err, string(body))
	}

	if response.Error != nil {
		return Channel{}, fmt.Errorf("youtube api error %d: %s", response.Error.Code, response.Error.Message)
	}

	if len(response.Items) == 0 {
		return Channel{}, fmt.Errorf("no channel found")
	}

	item := response.Items[0]
	return Channel{
		ID:           item.ID,
		Title:        item.Snippet.Title,
		ThumbnailURL: item.Snippet.Thumbnails.bestURL(),
		UploadsID:    item.ContentDetails.RelatedPlaylists.Uploads,
		Statistics:   item.Statistics,
	}, nil
}

func (t ThumbnailSet) bestURL() string {
	for _, thumbnail := range []*Thumbnail{t.Maxres, t.Standard, t.High, t.Medium, t.Default} {
		if thumbnail != nil && thumbnail.URL != "" {
			return thumbnail.URL
		}
	}

	return ""
}

func GetMyChannelLikes(accessToken, uploadsPlaylistID string) (string, error) {
	if strings.TrimSpace(accessToken) == "" {
		return "", fmt.Errorf("access token is empty")
	}

	if strings.TrimSpace(uploadsPlaylistID) == "" {
		channel, err := GetMyChannel(accessToken)
		if err != nil {
			return "", err
		}
		uploadsPlaylistID = channel.UploadsID
	}

	if uploadsPlaylistID == "" {
		return "", fmt.Errorf("uploads playlist is empty")
	}

	var totalLikes int64
	pageToken := ""

	for {
		videoIDs, nextPageToken, err := getPlaylistVideoIDs(accessToken, uploadsPlaylistID, pageToken)
		if err != nil {
			return "", err
		}

		if len(videoIDs) > 0 {
			likeCount, err := getVideosLikes(accessToken, videoIDs)
			if err != nil {
				return "", err
			}
			totalLikes += likeCount
		}

		if nextPageToken == "" {
			break
		}
		pageToken = nextPageToken
	}

	return strconv.FormatInt(totalLikes, 10), nil
}

func getPlaylistVideoIDs(accessToken, uploadsPlaylistID, pageToken string) ([]string, string, error) {
	req, err := http.NewRequest("GET", "https://www.googleapis.com/youtube/v3/playlistItems", nil)
	if err != nil {
		return nil, "", err
	}

	q := req.URL.Query()
	q.Add("part", "snippet")
	q.Add("playlistId", uploadsPlaylistID)
	q.Add("maxResults", "50")
	if pageToken != "" {
		q.Add("pageToken", pageToken)
	}
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	var response playlistItemsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, "", fmt.Errorf("playlist items parse failed: %w; body=%s", err, string(body))
	}

	if response.Error != nil {
		return nil, "", fmt.Errorf("youtube api error %d: %s", response.Error.Code, response.Error.Message)
	}

	videoIDs := make([]string, 0, len(response.Items))
	for _, item := range response.Items {
		videoID := strings.TrimSpace(item.Snippet.ResourceID.VideoID)
		if videoID != "" {
			videoIDs = append(videoIDs, videoID)
		}
	}

	return videoIDs, response.NextPageToken, nil
}

func getVideosLikes(accessToken string, videoIDs []string) (int64, error) {
	if len(videoIDs) == 0 {
		return 0, nil
	}

	req, err := http.NewRequest("GET", "https://www.googleapis.com/youtube/v3/videos", nil)
	if err != nil {
		return 0, err
	}

	q := req.URL.Query()
	q.Add("part", "statistics")
	q.Add("id", strings.Join(videoIDs, ","))
	q.Add("maxResults", "50")
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var response videosResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return 0, fmt.Errorf("videos parse failed: %w; body=%s", err, string(body))
	}

	if response.Error != nil {
		return 0, fmt.Errorf("youtube api error %d: %s", response.Error.Code, response.Error.Message)
	}

	var totalLikes int64
	for _, item := range response.Items {
		if item.Statistics.LikeCount == "" {
			continue
		}

		likes, err := strconv.ParseInt(item.Statistics.LikeCount, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid likeCount %q: %w", item.Statistics.LikeCount, err)
		}
		totalLikes += likes
	}

	return totalLikes, nil
}
