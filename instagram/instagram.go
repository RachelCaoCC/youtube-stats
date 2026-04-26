package instagram

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type APIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    int    `json:"code"`
}

type Account struct {
	ID                string
	Username          string
	ProfilePictureURL string
	FollowersCount    string
	MediaCount        string
}

type meResponse struct {
	ID                string    `json:"id,omitempty"`
	UserID            string    `json:"user_id,omitempty"`
	Username          string    `json:"username"`
	ProfilePictureURL string    `json:"profile_picture_url"`
	FollowersCount    int64     `json:"followers_count,omitempty"`
	MediaCount        int64     `json:"media_count,omitempty"`
	Error             *APIError `json:"error,omitempty"`
}

type mediaPageResponse struct {
	Data   []mediaItem `json:"data"`
	Paging mediaPaging `json:"paging"`
	Error  *APIError   `json:"error,omitempty"`
}

type mediaItem struct {
	ID        string `json:"id"`
	LikeCount int64  `json:"like_count,omitempty"`
}

type mediaPaging struct {
	Next string `json:"next"`
}

func GetMyAccount(accessToken string) (Account, error) {
	if strings.TrimSpace(accessToken) == "" {
		return Account{}, fmt.Errorf("instagram access token is empty")
	}

	account, err := getAccountByPath(accessToken, "/me", []string{
		"user_id",
		"username",
		"profile_picture_url",
		"followers_count",
		"media_count",
	})
	if err != nil {
		return Account{}, err
	}

	if account.ID != "" && account.FollowersCount != "" {
		return account, nil
	}

	if account.ID == "" {
		return account, nil
	}

	// Some Instagram responses return the ID first and require a second fetch for full profile fields.
	return getAccountByPath(accessToken, "/"+account.ID, []string{
		"id",
		"username",
		"profile_picture_url",
		"followers_count",
		"media_count",
	})
}

func GetMyLikes(accessToken string) (string, error) {
	if strings.TrimSpace(accessToken) == "" {
		return "", fmt.Errorf("instagram access token is empty")
	}

	nextURL := "https://graph.instagram.com/me/media?fields=id,like_count&limit=50"
	var totalLikes int64

	for nextURL != "" {
		req, err := http.NewRequest("GET", nextURL, nil)
		if err != nil {
			return "", err
		}

		query := req.URL.Query()
		if query.Get("access_token") == "" {
			query.Set("access_token", accessToken)
			req.URL.RawQuery = query.Encode()
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return "", readErr
		}

		var page mediaPageResponse
		if err := json.Unmarshal(body, &page); err != nil {
			return "", fmt.Errorf("instagram media parse failed: %w; body=%s", err, string(body))
		}

		if page.Error != nil {
			return "", fmt.Errorf("instagram api error %d: %s", page.Error.Code, page.Error.Message)
		}

		for _, item := range page.Data {
			totalLikes += item.LikeCount
		}

		nextURL = page.Paging.Next
	}

	return strconv.FormatInt(totalLikes, 10), nil
}

func getAccountByPath(accessToken, path string, fields []string) (Account, error) {
	req, err := http.NewRequest("GET", "https://graph.instagram.com"+path, nil)
	if err != nil {
		return Account{}, err
	}

	query := req.URL.Query()
	query.Set("fields", strings.Join(fields, ","))
	query.Set("access_token", accessToken)
	req.URL.RawQuery = query.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Account{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Account{}, err
	}

	var response meResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return Account{}, fmt.Errorf("instagram profile parse failed: %w; body=%s", err, string(body))
	}

	if response.Error != nil {
		return Account{}, fmt.Errorf("instagram api error %d: %s", response.Error.Code, response.Error.Message)
	}

	accountID := strings.TrimSpace(response.UserID)
	if accountID == "" {
		accountID = strings.TrimSpace(response.ID)
	}

	return Account{
		ID:                accountID,
		Username:          response.Username,
		ProfilePictureURL: response.ProfilePictureURL,
		FollowersCount:    formatCount(response.FollowersCount),
		MediaCount:        formatCount(response.MediaCount),
	}, nil
}

func formatCount(v int64) string {
	if v < 0 {
		return ""
	}

	return strconv.FormatInt(v, 10)
}
