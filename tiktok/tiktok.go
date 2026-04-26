package tiktok

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	LogID   string `json:"log_id"`
}

type Account struct {
	OpenID         string
	DisplayName    string
	Username       string
	AvatarURL      string
	FollowersCount string
	LikesCount     string
	VideoCount     string
}

type userInfoResponse struct {
	Data struct {
		User userInfoUser `json:"user"`
	} `json:"data"`
	Error APIError `json:"error"`
}

type userInfoUser struct {
	OpenID         string `json:"open_id"`
	DisplayName    string `json:"display_name"`
	Username       string `json:"username"`
	AvatarURL      string `json:"avatar_large_url"`
	FollowersCount int64  `json:"follower_count"`
	LikesCount     int64  `json:"likes_count"`
	VideoCount     int64  `json:"video_count"`
}

type videoListResponse struct {
	Data struct {
		Videos  []videoListItem `json:"videos"`
		Cursor  int64           `json:"cursor"`
		HasMore bool            `json:"has_more"`
	} `json:"data"`
	Error APIError `json:"error"`
}

type videoListItem struct {
	ID string `json:"id"`
}

type videoQueryResponse struct {
	Data struct {
		Videos []videoQueryItem `json:"videos"`
	} `json:"data"`
	Error APIError `json:"error"`
}

type videoQueryItem struct {
	ID        string `json:"id"`
	ViewCount int64  `json:"view_count"`
}

func GetMyAccount(accessToken string) (Account, error) {
	if strings.TrimSpace(accessToken) == "" {
		return Account{}, fmt.Errorf("tiktok access token is empty")
	}

	req, err := http.NewRequest("GET", "https://open.tiktokapis.com/v2/user/info/", nil)
	if err != nil {
		return Account{}, err
	}

	query := req.URL.Query()
	query.Set("fields", "open_id,display_name,avatar_large_url,username,follower_count,likes_count,video_count")
	req.URL.RawQuery = query.Encode()
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Account{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Account{}, err
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return Account{}, parseError(body, resp.Status)
	}

	var response userInfoResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return Account{}, fmt.Errorf("tiktok user parse failed: %w; body=%s", err, string(body))
	}

	if response.Error.Code != "" && response.Error.Code != "ok" {
		return Account{}, fmt.Errorf("tiktok api error %s: %s", response.Error.Code, response.Error.Message)
	}

	return Account{
		OpenID:         response.Data.User.OpenID,
		DisplayName:    response.Data.User.DisplayName,
		Username:       response.Data.User.Username,
		AvatarURL:      response.Data.User.AvatarURL,
		FollowersCount: strconv.FormatInt(response.Data.User.FollowersCount, 10),
		LikesCount:     strconv.FormatInt(response.Data.User.LikesCount, 10),
		VideoCount:     strconv.FormatInt(response.Data.User.VideoCount, 10),
	}, nil
}

func GetMyTotalViews(accessToken string) (string, error) {
	if strings.TrimSpace(accessToken) == "" {
		return "", fmt.Errorf("tiktok access token is empty")
	}

	var totalViews int64
	var cursor int64

	for {
		ids, nextCursor, hasMore, err := listVideoIDs(accessToken, cursor)
		if err != nil {
			return "", err
		}

		if len(ids) > 0 {
			pageViews, err := queryVideoViews(accessToken, ids)
			if err != nil {
				return "", err
			}
			totalViews += pageViews
		}

		if !hasMore {
			break
		}
		cursor = nextCursor
	}

	return strconv.FormatInt(totalViews, 10), nil
}

func listVideoIDs(accessToken string, cursor int64) ([]string, int64, bool, error) {
	body := map[string]any{
		"max_count": 20,
	}
	if cursor > 0 {
		body["cursor"] = cursor
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, 0, false, err
	}

	req, err := http.NewRequest("POST", "https://open.tiktokapis.com/v2/video/list/?fields=id", bytes.NewReader(payload))
	if err != nil {
		return nil, 0, false, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, false, err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, false, err
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, 0, false, parseError(responseBody, resp.Status)
	}

	var response videoListResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, 0, false, fmt.Errorf("tiktok video list parse failed: %w; body=%s", err, string(responseBody))
	}

	if response.Error.Code != "" && response.Error.Code != "ok" {
		return nil, 0, false, fmt.Errorf("tiktok api error %s: %s", response.Error.Code, response.Error.Message)
	}

	ids := make([]string, 0, len(response.Data.Videos))
	for _, video := range response.Data.Videos {
		if strings.TrimSpace(video.ID) != "" {
			ids = append(ids, video.ID)
		}
	}

	return ids, response.Data.Cursor, response.Data.HasMore, nil
}

func queryVideoViews(accessToken string, videoIDs []string) (int64, error) {
	payload, err := json.Marshal(map[string]any{
		"filters": map[string]any{
			"video_ids": videoIDs,
		},
	})
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequest("POST", "https://open.tiktokapis.com/v2/video/query/?fields=id,view_count", bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return 0, parseError(responseBody, resp.Status)
	}

	var response videoQueryResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return 0, fmt.Errorf("tiktok video query parse failed: %w; body=%s", err, string(responseBody))
	}

	if response.Error.Code != "" && response.Error.Code != "ok" {
		return 0, fmt.Errorf("tiktok api error %s: %s", response.Error.Code, response.Error.Message)
	}

	var total int64
	for _, video := range response.Data.Videos {
		total += video.ViewCount
	}

	return total, nil
}

func parseError(body []byte, status string) error {
	var apiErr struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
		LogID            string `json:"log_id"`
	}
	if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error != "" {
		return fmt.Errorf("tiktok api request failed: %s (%s)", apiErr.Error, apiErr.ErrorDescription)
	}

	var responseErr struct {
		Error APIError `json:"error"`
	}
	if err := json.Unmarshal(body, &responseErr); err == nil && responseErr.Error.Code != "" && responseErr.Error.Code != "ok" {
		return fmt.Errorf("tiktok api error %s: %s", responseErr.Error.Code, responseErr.Error.Message)
	}

	return fmt.Errorf("tiktok api request failed: %s", status)
}
