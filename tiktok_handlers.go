package main

import (
	"errors"
	"log"
	"net/http"
	"time"

	"youtube-stats/appsession"
	"youtube-stats/tiktok"
)

type tiktokStatusResponse struct {
	OAuthConfigured bool     `json:"oauthConfigured"`
	Connected       bool     `json:"connected"`
	OpenID          string   `json:"openId,omitempty"`
	DisplayName     string   `json:"displayName,omitempty"`
	Username        string   `json:"username,omitempty"`
	AvatarURL       string   `json:"avatarUrl,omitempty"`
	LiveMetrics     []string `json:"liveMetrics"`
	Error           string   `json:"error,omitempty"`
}

type tiktokStatsResponse struct {
	tiktokStatusResponse
	FollowersCount string `json:"followersCount,omitempty"`
	LikeCount      string `json:"likeCount,omitempty"`
	ViewCount      string `json:"viewCount,omitempty"`
	FetchedAt      string `json:"fetchedAt,omitempty"`
}

var errTikTokNotConnected = errors.New("tiktok account is not connected")

const tiktokViewsCacheTTL = 10 * time.Minute

func tiktokAuthStart(w http.ResponseWriter, r *http.Request) {
	session := appsession.GetOrCreate(w, r)

	if !tiktok.OAuthConfigured() {
		http.Redirect(w, r, redirectHomeWithStatus("tiktok", "setup"), http.StatusSeeOther)
		return
	}

	session.TikTokOAuthState = appsession.NewStateToken()

	authURL, err := tiktok.BuildAuthURL(session.TikTokOAuthState)
	if err != nil {
		log.Println("TikTok BuildAuthURL error:", err)
		http.Redirect(w, r, redirectHomeWithStatus("tiktok", "setup"), http.StatusSeeOther)
		return
	}

	appsession.Save(session)
	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

func tiktokAuthCallback(w http.ResponseWriter, r *http.Request) {
	session, ok := appsession.Get(r)
	if !ok {
		http.Redirect(w, r, redirectHomeWithStatus("tiktok", "session"), http.StatusSeeOther)
		return
	}

	if queryError := r.URL.Query().Get("error"); queryError != "" {
		http.Redirect(w, r, redirectHomeWithStatus("tiktok", "cancelled"), http.StatusSeeOther)
		return
	}

	if gotState := r.URL.Query().Get("state"); gotState == "" || gotState != session.TikTokOAuthState {
		http.Redirect(w, r, redirectHomeWithStatus("tiktok", "state"), http.StatusSeeOther)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, redirectHomeWithStatus("tiktok", "code"), http.StatusSeeOther)
		return
	}

	token, err := tiktok.ExchangeCode(code)
	if err != nil {
		log.Println("TikTok ExchangeCode error:", err)
		http.Redirect(w, r, redirectHomeWithStatus("tiktok", "token"), http.StatusSeeOther)
		return
	}

	if token.RefreshToken == "" && session.TikTok != nil {
		token.RefreshToken = session.TikTok.RefreshToken
	}

	account, err := tiktok.GetMyAccount(token.AccessToken)
	if err != nil {
		log.Println("TikTok GetMyAccount error:", err)
		http.Redirect(w, r, redirectHomeWithStatus("tiktok", "account"), http.StatusSeeOther)
		return
	}

	session.TikTokOAuthState = ""
	session.TikTok = &appsession.TikTokConnection{
		AccessToken:    token.AccessToken,
		RefreshToken:   token.RefreshToken,
		TokenType:      token.TokenType,
		Expiry:         token.ExpiryTime(),
		RefreshExpiry:  token.RefreshExpiryTime(),
		OpenID:         account.OpenID,
		DisplayName:    account.DisplayName,
		Username:       account.Username,
		AvatarURL:      account.AvatarURL,
		FollowersCount: account.FollowersCount,
		LikesCount:     account.LikesCount,
	}
	appsession.Save(session)

	http.Redirect(w, r, redirectHomeWithStatus("tiktok", "connected"), http.StatusSeeOther)
}

func tiktokDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	session, ok := appsession.Get(r)
	if !ok {
		writeJSON(w, http.StatusOK, tiktokStatusResponse{
			OAuthConfigured: tiktok.OAuthConfigured(),
			Connected:       false,
			LiveMetrics:     []string{"FOLLOWERS", "VIEWS", "LIKES"},
		})
		return
	}

	session.TikTok = nil
	session.TikTokOAuthState = ""
	appsession.Save(session)

	writeJSON(w, http.StatusOK, tiktokStatusResponse{
		OAuthConfigured: tiktok.OAuthConfigured(),
		Connected:       false,
		LiveMetrics:     []string{"FOLLOWERS", "VIEWS", "LIKES"},
	})
}

func tiktokStatus(w http.ResponseWriter, r *http.Request) {
	response := tiktokStatusResponse{
		OAuthConfigured: tiktok.OAuthConfigured(),
		Connected:       false,
		LiveMetrics:     []string{"FOLLOWERS", "VIEWS", "LIKES"},
	}

	session := appsession.GetOrCreate(w, r)
	if session.TikTok != nil {
		response.Connected = true
		response.OpenID = session.TikTok.OpenID
		response.DisplayName = session.TikTok.DisplayName
		response.Username = session.TikTok.Username
		response.AvatarURL = session.TikTok.AvatarURL
	}

	writeJSON(w, http.StatusOK, response)
}

func tiktokStats(w http.ResponseWriter, r *http.Request) {
	session, ok := appsession.Get(r)
	if !ok || session.TikTok == nil {
		writeJSON(w, http.StatusUnauthorized, tiktokStatsResponse{
			tiktokStatusResponse: tiktokStatusResponse{
				OAuthConfigured: tiktok.OAuthConfigured(),
				Connected:       false,
				LiveMetrics:     []string{"FOLLOWERS", "VIEWS", "LIKES"},
				Error:           errTikTokNotConnected.Error(),
			},
		})
		return
	}

	updatedSession, account, viewCount, err := ensureFreshTikTokAccount(session)
	if err != nil {
		log.Println("ensureFreshTikTokAccount error:", err)
		if errors.Is(err, errTikTokNotConnected) {
			writeJSON(w, http.StatusUnauthorized, tiktokStatsResponse{
				tiktokStatusResponse: tiktokStatusResponse{
					OAuthConfigured: tiktok.OAuthConfigured(),
					Connected:       false,
					LiveMetrics:     []string{"FOLLOWERS", "VIEWS", "LIKES"},
					Error:           err.Error(),
				},
			})
			return
		}

		writeJSON(w, http.StatusBadGateway, tiktokStatsResponse{
			tiktokStatusResponse: tiktokStatusResponse{
				OAuthConfigured: tiktok.OAuthConfigured(),
				Connected:       updatedSession.TikTok != nil,
				OpenID:          currentTikTokOpenID(updatedSession),
				DisplayName:     currentTikTokDisplayName(updatedSession),
				Username:        currentTikTokUsername(updatedSession),
				AvatarURL:       currentTikTokAvatar(updatedSession),
				LiveMetrics:     []string{"FOLLOWERS", "VIEWS", "LIKES"},
				Error:           err.Error(),
			},
		})
		return
	}

	appsession.Save(updatedSession)
	writeJSON(w, http.StatusOK, tiktokStatsResponse{
		tiktokStatusResponse: tiktokStatusResponse{
			OAuthConfigured: tiktok.OAuthConfigured(),
			Connected:       true,
			OpenID:          account.OpenID,
			DisplayName:     account.DisplayName,
			Username:        account.Username,
			AvatarURL:       account.AvatarURL,
			LiveMetrics:     []string{"FOLLOWERS", "VIEWS", "LIKES"},
		},
		FollowersCount: account.FollowersCount,
		LikeCount:      account.LikesCount,
		ViewCount:      viewCount,
		FetchedAt:      time.Now().Format(time.RFC3339),
	})
}

func ensureFreshTikTokAccount(session appsession.Session) (appsession.Session, tiktok.Account, string, error) {
	if session.TikTok == nil {
		return session, tiktok.Account{}, "", errTikTokNotConnected
	}

	if session.TikTok.RefreshToken == "" {
		session.TikTok = nil
		appsession.Save(session)
		return session, tiktok.Account{}, "", errTikTokNotConnected
	}

	if !session.TikTok.RefreshExpiry.IsZero() && time.Now().After(session.TikTok.RefreshExpiry) {
		session.TikTok = nil
		appsession.Save(session)
		return session, tiktok.Account{}, "", errTikTokNotConnected
	}

	if time.Until(session.TikTok.Expiry) <= 5*time.Minute {
		token, err := tiktok.RefreshAccessToken(session.TikTok.RefreshToken)
		if err != nil {
			session.TikTok = nil
			appsession.Save(session)
			return session, tiktok.Account{}, "", errTikTokNotConnected
		}

		if token.RefreshToken == "" {
			token.RefreshToken = session.TikTok.RefreshToken
		}

		session.TikTok.AccessToken = token.AccessToken
		session.TikTok.RefreshToken = token.RefreshToken
		session.TikTok.TokenType = token.TokenType
		session.TikTok.Expiry = token.ExpiryTime()
		session.TikTok.RefreshExpiry = token.RefreshExpiryTime()
		if token.OpenID != "" {
			session.TikTok.OpenID = token.OpenID
		}
	}

	account, err := tiktok.GetMyAccount(session.TikTok.AccessToken)
	if err != nil {
		return session, tiktok.Account{}, "", err
	}

	session.TikTok.OpenID = account.OpenID
	session.TikTok.DisplayName = account.DisplayName
	session.TikTok.Username = account.Username
	session.TikTok.AvatarURL = account.AvatarURL
	session.TikTok.FollowersCount = account.FollowersCount
	session.TikTok.LikesCount = account.LikesCount

	if session.TikTok.ViewsCount == "" || time.Since(session.TikTok.ViewsFetchedAt) >= tiktokViewsCacheTTL {
		viewCount, err := tiktok.GetMyTotalViews(session.TikTok.AccessToken)
		if err != nil {
			if session.TikTok.ViewsCount != "" {
				return session, account, session.TikTok.ViewsCount, nil
			}
			return session, tiktok.Account{}, "", err
		}

		session.TikTok.ViewsCount = viewCount
		session.TikTok.ViewsFetchedAt = time.Now()
	}

	return session, account, session.TikTok.ViewsCount, nil
}

func currentTikTokOpenID(session appsession.Session) string {
	if session.TikTok == nil {
		return ""
	}
	return session.TikTok.OpenID
}

func currentTikTokDisplayName(session appsession.Session) string {
	if session.TikTok == nil {
		return ""
	}
	return session.TikTok.DisplayName
}

func currentTikTokUsername(session appsession.Session) string {
	if session.TikTok == nil {
		return ""
	}
	return session.TikTok.Username
}

func currentTikTokAvatar(session appsession.Session) string {
	if session.TikTok == nil {
		return ""
	}
	return session.TikTok.AvatarURL
}

func init() {
	if !tiktok.OAuthConfigured() {
		log.Println("TikTok OAuth disabled until TIKTOK_CLIENT_KEY, TIKTOK_CLIENT_SECRET, and TIKTOK_REDIRECT_URL are set")
	}
}
