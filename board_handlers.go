package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"youtube-stats/appsession"
	"youtube-stats/board"
)

var (
	validPlatformKeys = map[string]struct{}{
		"instagram": {},
		"tiktok":    {},
		"facebook":  {},
		"youtube":   {},
	}
	validTopMetricLabels = map[string]struct{}{
		"LIKES": {},
		"VIEWS": {},
	}
)

type boardRecordResponse struct {
	Exists           bool     `json:"exists"`
	Slug             string   `json:"slug,omitempty"`
	PublicURL        string   `json:"publicUrl,omitempty"`
	PlatformKeys     []string `json:"platformKeys,omitempty"`
	TopMetricLabels  []string `json:"topMetricLabels,omitempty"`
	UpdatedAt        string   `json:"updatedAt,omitempty"`
	Error            string   `json:"error,omitempty"`
	PublicViewerNote string   `json:"publicViewerNote,omitempty"`
}

type publishBoardRequest struct {
	PlatformKeys    []string `json:"platformKeys"`
	TopMetricLabels []string `json:"topMetricLabels"`
}

type publicBoardResponse struct {
	Slug            string                        `json:"slug"`
	PublicURL       string                        `json:"publicUrl"`
	PlatformKeys    []string                      `json:"platformKeys"`
	TopMetricLabels []string                      `json:"topMetricLabels"`
	PlatformStates  map[string]publicPlatformData `json:"platformStates"`
	UpdatedAt       string                        `json:"updatedAt"`
	Warning         string                        `json:"warning,omitempty"`
}

type publicPlatformData struct {
	Live      bool   `json:"live"`
	Followers string `json:"followers,omitempty"`
	Likes     string `json:"likes,omitempty"`
	Views     string `json:"views,omitempty"`
}

func boardPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := boardSlugFromPath(r.URL.Path, "/board/"); !ok {
		http.NotFound(w, r)
		return
	}

	http.ServeFile(w, r, "index.html")
}

func myBoard(w http.ResponseWriter, r *http.Request) {
	session := appsession.GetOrCreate(w, r)
	record, ok := board.GetBySession(session.ID)
	if !ok {
		writeJSON(w, http.StatusOK, boardRecordResponse{
			Exists:           false,
			PublicViewerNote: "Publish a board and share the link. Visitors on that link will only see the read-only counter display.",
		})
		return
	}

	writeJSON(w, http.StatusOK, boardRecordResponse{
		Exists:           true,
		Slug:             record.Slug,
		PublicURL:        publicBoardURL(r, record.Slug),
		PlatformKeys:     append([]string(nil), record.PlatformKeys...),
		TopMetricLabels:  append([]string(nil), record.TopMetricLabels...),
		UpdatedAt:        record.UpdatedAt.Format(timeJSONFormat),
		PublicViewerNote: "Visitors on the public board never see your connect buttons. They only see the display.",
	})
}

func publishBoard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	session := appsession.GetOrCreate(w, r)

	var request publishBoardRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, boardRecordResponse{
			Error: "The board settings could not be read.",
		})
		return
	}

	platformKeys, topMetricLabels, err := normalizeBoardSelection(request.PlatformKeys, request.TopMetricLabels)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, boardRecordResponse{
			Error: err.Error(),
		})
		return
	}

	record, err := board.UpsertForSession(session.ID, platformKeys, topMetricLabels)
	if err != nil {
		log.Println("board publish error:", err)
		writeJSON(w, http.StatusInternalServerError, boardRecordResponse{
			Error: "The public board could not be saved right now.",
		})
		return
	}

	appsession.Pin(session.ID)

	writeJSON(w, http.StatusOK, boardRecordResponse{
		Exists:           true,
		Slug:             record.Slug,
		PublicURL:        publicBoardURL(r, record.Slug),
		PlatformKeys:     append([]string(nil), record.PlatformKeys...),
		TopMetricLabels:  append([]string(nil), record.TopMetricLabels...),
		UpdatedAt:        record.UpdatedAt.Format(timeJSONFormat),
		PublicViewerNote: "Visitors on the public board never see your connect buttons. They only see the display.",
	})
}

func publicBoardData(w http.ResponseWriter, r *http.Request) {
	slug, ok := boardSlugFromPath(r.URL.Path, "/api/boards/")
	if !ok || slug == "me" || slug == "publish" {
		http.NotFound(w, r)
		return
	}

	record, ok := board.GetBySlug(slug)
	if !ok {
		writeJSON(w, http.StatusNotFound, boardRecordResponse{
			Error: "That public board was not found.",
		})
		return
	}

	response := publicBoardResponse{
		Slug:            record.Slug,
		PublicURL:       publicBoardURL(r, record.Slug),
		PlatformKeys:    append([]string(nil), record.PlatformKeys...),
		TopMetricLabels: append([]string(nil), record.TopMetricLabels...),
		PlatformStates:  map[string]publicPlatformData{},
		UpdatedAt:       record.UpdatedAt.Format(timeJSONFormat),
	}

	session, ok := appsession.GetByID(record.SessionID)
	if !ok {
		response.Warning = "The creator connection for this board is unavailable, so connected platforms are showing their fallback numbers right now."
		writeJSON(w, http.StatusOK, response)
		return
	}

	if containsString(record.PlatformKeys, "youtube") && session.YouTube != nil {
		updatedSession, channel, likesCount, err := ensureFreshYouTubeChannel(session)
		if err == nil {
			session = updatedSession
			appsession.Save(updatedSession)
			response.PlatformStates["youtube"] = publicPlatformData{
				Live:      true,
				Followers: channel.Statistics.SubscriberCount,
				Views:     channel.Statistics.ViewCount,
				Likes:     likesCount,
			}
		} else if !errors.Is(err, errYouTubeNotConnected) {
			log.Println("public board youtube refresh error:", err)
			response.Warning = appendWarning(response.Warning, "YouTube live data could not be refreshed, so fallback numbers may be showing.")
		}
	}

	if containsString(record.PlatformKeys, "instagram") && session.Instagram != nil {
		updatedSession, account, likesCount, err := ensureFreshInstagramAccount(session)
		if err == nil {
			session = updatedSession
			appsession.Save(updatedSession)
			response.PlatformStates["instagram"] = publicPlatformData{
				Live:      true,
				Followers: account.FollowersCount,
				Likes:     likesCount,
			}
		} else if !errors.Is(err, errInstagramNotConnected) {
			log.Println("public board instagram refresh error:", err)
			response.Warning = appendWarning(response.Warning, "Instagram live data could not be refreshed, so fallback numbers may be showing.")
		}
	}

	if containsString(record.PlatformKeys, "tiktok") && session.TikTok != nil {
		updatedSession, account, viewCount, err := ensureFreshTikTokAccount(session)
		if err == nil {
			session = updatedSession
			appsession.Save(updatedSession)
			response.PlatformStates["tiktok"] = publicPlatformData{
				Live:      true,
				Followers: account.FollowersCount,
				Likes:     account.LikesCount,
				Views:     viewCount,
			}
		} else if !errors.Is(err, errTikTokNotConnected) {
			log.Println("public board tiktok refresh error:", err)
			response.Warning = appendWarning(response.Warning, "TikTok live data could not be refreshed, so fallback numbers may be showing.")
		}
	}

	writeJSON(w, http.StatusOK, response)
}

func normalizeBoardSelection(platformKeys, topMetricLabels []string) ([]string, []string, error) {
	platformKeys = uniqueAllowed(platformKeys, validPlatformKeys)
	topMetricLabels = uniqueAllowed(topMetricLabels, validTopMetricLabels)

	if len(platformKeys) == 0 {
		return nil, nil, errors.New("Choose at least one platform before publishing the public board.")
	}
	if len(topMetricLabels) == 0 {
		return nil, nil, errors.New("Choose at least one top-row metric before publishing the public board.")
	}

	return platformKeys, topMetricLabels, nil
}

func uniqueAllowed(values []string, allowed map[string]struct{}) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))

	for _, value := range values {
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "likes" || value == "views" {
			value = strings.ToUpper(value)
		}

		if _, ok := allowed[value]; !ok {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}

		seen[value] = struct{}{}
		result = append(result, value)
	}

	return result
}

func publicBoardURL(r *http.Request, slug string) string {
	return externalBaseURL(r) + "/board/" + url.PathEscape(slug)
}

func externalBaseURL(r *http.Request) string {
	if baseURL := strings.TrimSpace(strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/")); baseURL != "" {
		return baseURL
	}

	scheme := "http"
	if requestIsHTTPS(r) {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = strings.Split(forwarded, ",")[0]
	}

	host := strings.TrimSpace(r.Host)
	if forwardedHost := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
		host = strings.Split(forwardedHost, ",")[0]
	}

	return scheme + "://" + host
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}

	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func boardSlugFromPath(path, prefix string) (string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}

	slug := strings.TrimPrefix(path, prefix)
	if slug == "" || strings.Contains(slug, "/") {
		return "", false
	}

	return slug, true
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func appendWarning(existing, next string) string {
	if next == "" {
		return existing
	}
	if existing == "" {
		return next
	}
	return existing + " " + next
}

const timeJSONFormat = "2006-01-02T15:04:05Z07:00"
