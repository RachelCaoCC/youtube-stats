package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"time"

	"youtube-stats/appsession"
	"youtube-stats/youtube"
)

type youtubeStatusResponse struct {
	OAuthConfigured     bool     `json:"oauthConfigured"`
	Connected           bool     `json:"connected"`
	ChannelID           string   `json:"channelId,omitempty"`
	ChannelTitle        string   `json:"channelTitle,omitempty"`
	ChannelThumbnailURL string   `json:"channelThumbnailUrl,omitempty"`
	PrototypeLikes      bool     `json:"prototypeLikes"`
	LiveMetrics         []string `json:"liveMetrics"`
	Error               string   `json:"error,omitempty"`
}

type youtubeStatsResponse struct {
	youtubeStatusResponse
	SubscriberCount string `json:"subscriberCount,omitempty"`
	ViewCount       string `json:"viewCount,omitempty"`
	LikeCount       string `json:"likeCount,omitempty"`
	FetchedAt       string `json:"fetchedAt,omitempty"`
}

var errYouTubeNotConnected = errors.New("youtube account is not connected")

const youtubeLikesCacheTTL = 10 * time.Minute

func youtubeAuthStart(w http.ResponseWriter, r *http.Request) {
	session := appsession.GetOrCreate(w, r)

	if !youtube.OAuthConfigured() {
		http.Redirect(w, r, redirectHomeWithStatus("youtube", "setup"), http.StatusSeeOther)
		return
	}

	session.YouTubeOAuthState = appsession.NewStateToken()

	authURL, err := youtube.BuildAuthURL(session.YouTubeOAuthState)
	if err != nil {
		log.Println("BuildAuthURL error:", err)
		http.Redirect(w, r, redirectHomeWithStatus("youtube", "setup"), http.StatusSeeOther)
		return
	}

	appsession.Save(session)
	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

func youtubeAuthCallback(w http.ResponseWriter, r *http.Request) {
	session, ok := appsession.Get(r)
	if !ok {
		http.Redirect(w, r, redirectHomeWithStatus("youtube", "session"), http.StatusSeeOther)
		return
	}

	if queryError := r.URL.Query().Get("error"); queryError != "" {
		http.Redirect(w, r, redirectHomeWithStatus("youtube", "cancelled"), http.StatusSeeOther)
		return
	}

	if gotState := r.URL.Query().Get("state"); gotState == "" || gotState != session.YouTubeOAuthState {
		http.Redirect(w, r, redirectHomeWithStatus("youtube", "state"), http.StatusSeeOther)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, redirectHomeWithStatus("youtube", "code"), http.StatusSeeOther)
		return
	}

	token, err := youtube.ExchangeCode(code)
	if err != nil {
		log.Println("ExchangeCode error:", err)
		http.Redirect(w, r, redirectHomeWithStatus("youtube", "token"), http.StatusSeeOther)
		return
	}

	if token.RefreshToken == "" && session.YouTube != nil {
		token.RefreshToken = session.YouTube.RefreshToken
	}

	channel, err := youtube.GetMyChannel(token.AccessToken)
	if err != nil {
		log.Println("GetMyChannel error:", err)
		http.Redirect(w, r, redirectHomeWithStatus("youtube", "channel"), http.StatusSeeOther)
		return
	}

	session.YouTubeOAuthState = ""
	session.YouTube = &appsession.YouTubeConnection{
		AccessToken:     token.AccessToken,
		RefreshToken:    token.RefreshToken,
		TokenType:       token.TokenType,
		Expiry:          token.ExpiryTime(),
		ChannelID:       channel.ID,
		ChannelTitle:    channel.Title,
		ChannelThumbURL: channel.ThumbnailURL,
	}
	appsession.Save(session)

	http.Redirect(w, r, redirectHomeWithStatus("youtube", "connected"), http.StatusSeeOther)
}

func youtubeDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	session, ok := appsession.Get(r)
	if !ok {
		writeJSON(w, http.StatusOK, youtubeStatusResponse{
			OAuthConfigured: youtube.OAuthConfigured(),
			Connected:       false,
			PrototypeLikes:  false,
			LiveMetrics:     []string{"FOLLOWERS", "VIEWS", "LIKES"},
		})
		return
	}

	session.YouTube = nil
	session.YouTubeOAuthState = ""
	appsession.Save(session)

	writeJSON(w, http.StatusOK, youtubeStatusResponse{
		OAuthConfigured: youtube.OAuthConfigured(),
		Connected:       false,
		PrototypeLikes:  false,
		LiveMetrics:     []string{"FOLLOWERS", "VIEWS", "LIKES"},
	})
}

func youtubeStatus(w http.ResponseWriter, r *http.Request) {
	response := youtubeStatusResponse{
		OAuthConfigured: youtube.OAuthConfigured(),
		Connected:       false,
		PrototypeLikes:  false,
		LiveMetrics:     []string{"FOLLOWERS", "VIEWS", "LIKES"},
	}

	session := appsession.GetOrCreate(w, r)
	if session.YouTube != nil {
		response.Connected = true
		response.ChannelID = session.YouTube.ChannelID
		response.ChannelTitle = session.YouTube.ChannelTitle
		response.ChannelThumbnailURL = session.YouTube.ChannelThumbURL
	}

	writeJSON(w, http.StatusOK, response)
}

func youtubeStats(w http.ResponseWriter, r *http.Request) {
	session, ok := appsession.Get(r)
	if !ok || session.YouTube == nil {
		writeJSON(w, http.StatusUnauthorized, youtubeStatsResponse{
			youtubeStatusResponse: youtubeStatusResponse{
				OAuthConfigured: youtube.OAuthConfigured(),
				Connected:       false,
				PrototypeLikes:  false,
				LiveMetrics:     []string{"FOLLOWERS", "VIEWS", "LIKES"},
				Error:           errYouTubeNotConnected.Error(),
			},
		})
		return
	}

	updatedSession, channel, likesCount, err := ensureFreshYouTubeChannel(session)
	if err != nil {
		log.Println("ensureFreshYouTubeChannel error:", err)
		if errors.Is(err, errYouTubeNotConnected) {
			writeJSON(w, http.StatusUnauthorized, youtubeStatsResponse{
				youtubeStatusResponse: youtubeStatusResponse{
					OAuthConfigured: youtube.OAuthConfigured(),
					Connected:       false,
					PrototypeLikes:  false,
					LiveMetrics:     []string{"FOLLOWERS", "VIEWS", "LIKES"},
					Error:           err.Error(),
				},
			})
			return
		}

		writeJSON(w, http.StatusBadGateway, youtubeStatsResponse{
			youtubeStatusResponse: youtubeStatusResponse{
				OAuthConfigured:     youtube.OAuthConfigured(),
				Connected:           updatedSession.YouTube != nil,
				ChannelID:           currentChannelID(updatedSession),
				ChannelTitle:        currentChannelTitle(updatedSession),
				ChannelThumbnailURL: currentChannelThumb(updatedSession),
				PrototypeLikes:      false,
				LiveMetrics:         []string{"FOLLOWERS", "VIEWS", "LIKES"},
				Error:               err.Error(),
			},
		})
		return
	}

	appsession.Save(updatedSession)
	writeJSON(w, http.StatusOK, youtubeStatsResponse{
		youtubeStatusResponse: youtubeStatusResponse{
			OAuthConfigured:     youtube.OAuthConfigured(),
			Connected:           true,
			ChannelID:           channel.ID,
			ChannelTitle:        channel.Title,
			ChannelThumbnailURL: channel.ThumbnailURL,
			PrototypeLikes:      false,
			LiveMetrics:         []string{"FOLLOWERS", "VIEWS", "LIKES"},
		},
		SubscriberCount: channel.Statistics.SubscriberCount,
		ViewCount:       channel.Statistics.ViewCount,
		LikeCount:       likesCount,
		FetchedAt:       time.Now().Format(time.RFC3339),
	})
}

func ensureFreshYouTubeChannel(session appsession.Session) (appsession.Session, youtube.Channel, string, error) {
	if session.YouTube == nil {
		return session, youtube.Channel{}, "", errYouTubeNotConnected
	}

	if session.YouTube.RefreshToken == "" {
		session.YouTube = nil
		appsession.Save(session)
		return session, youtube.Channel{}, "", errYouTubeNotConnected
	}

	if time.Until(session.YouTube.Expiry) <= 30*time.Second {
		token, err := youtube.RefreshAccessToken(session.YouTube.RefreshToken)
		if err != nil {
			session.YouTube = nil
			appsession.Save(session)
			return session, youtube.Channel{}, "", errYouTubeNotConnected
		}

		if token.RefreshToken == "" {
			token.RefreshToken = session.YouTube.RefreshToken
		}

		session.YouTube.AccessToken = token.AccessToken
		session.YouTube.RefreshToken = token.RefreshToken
		session.YouTube.TokenType = token.TokenType
		session.YouTube.Expiry = token.ExpiryTime()
	}

	channel, err := youtube.GetMyChannel(session.YouTube.AccessToken)
	if err != nil {
		return session, youtube.Channel{}, "", err
	}

	session.YouTube.ChannelID = channel.ID
	session.YouTube.ChannelTitle = channel.Title
	session.YouTube.ChannelThumbURL = channel.ThumbnailURL

	// Calculating likes can span many uploaded videos, so keep a short cache in the session.
	if session.YouTube.LikesCount == "" || time.Since(session.YouTube.LikesFetchedAt) >= youtubeLikesCacheTTL {
		likesCount, err := youtube.GetMyChannelLikes(session.YouTube.AccessToken, channel.UploadsID)
		if err != nil {
			if session.YouTube.LikesCount != "" {
				return session, channel, session.YouTube.LikesCount, nil
			}
			return session, youtube.Channel{}, "", err
		}

		session.YouTube.LikesCount = likesCount
		session.YouTube.LikesFetchedAt = time.Now()
	}

	return session, channel, session.YouTube.LikesCount, nil
}

func redirectHomeWithStatus(provider, status string) string {
	query := url.Values{}
	query.Set(provider, status)
	return dashboardPath + "?" + query.Encode()
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Println("json encode error:", err)
	}
}

func currentChannelID(session appsession.Session) string {
	if session.YouTube == nil {
		return ""
	}
	return session.YouTube.ChannelID
}

func currentChannelTitle(session appsession.Session) string {
	if session.YouTube == nil {
		return ""
	}
	return session.YouTube.ChannelTitle
}

func currentChannelThumb(session appsession.Session) string {
	if session.YouTube == nil {
		return ""
	}
	return session.YouTube.ChannelThumbURL
}

func init() {
	if !youtube.OAuthConfigured() {
		log.Println("YouTube OAuth disabled until GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, and YOUTUBE_REDIRECT_URL are set")
	}
}
