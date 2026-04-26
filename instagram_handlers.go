package main

import (
	"errors"
	"log"
	"net/http"
	"time"

	"youtube-stats/appsession"
	"youtube-stats/instagram"
)

type instagramStatusResponse struct {
	OAuthConfigured   bool     `json:"oauthConfigured"`
	Connected         bool     `json:"connected"`
	UserID            string   `json:"userId,omitempty"`
	Username          string   `json:"username,omitempty"`
	ProfilePictureURL string   `json:"profilePictureUrl,omitempty"`
	LiveMetrics       []string `json:"liveMetrics"`
	Error             string   `json:"error,omitempty"`
}

type instagramStatsResponse struct {
	instagramStatusResponse
	FollowersCount string `json:"followersCount,omitempty"`
	LikeCount      string `json:"likeCount,omitempty"`
	FetchedAt      string `json:"fetchedAt,omitempty"`
}

var errInstagramNotConnected = errors.New("instagram account is not connected")

const instagramLikesCacheTTL = 10 * time.Minute

func instagramAuthStart(w http.ResponseWriter, r *http.Request) {
	session := appsession.GetOrCreate(w, r)

	if !instagram.OAuthConfigured() {
		http.Redirect(w, r, redirectHomeWithStatus("instagram", "setup"), http.StatusSeeOther)
		return
	}

	session.InstagramOAuthState = appsession.NewStateToken()

	authURL, err := instagram.BuildAuthURL(session.InstagramOAuthState)
	if err != nil {
		log.Println("Instagram BuildAuthURL error:", err)
		http.Redirect(w, r, redirectHomeWithStatus("instagram", "setup"), http.StatusSeeOther)
		return
	}

	appsession.Save(session)
	http.Redirect(w, r, authURL, http.StatusSeeOther)
}

func instagramAuthCallback(w http.ResponseWriter, r *http.Request) {
	session, ok := appsession.Get(r)
	if !ok {
		http.Redirect(w, r, redirectHomeWithStatus("instagram", "session"), http.StatusSeeOther)
		return
	}

	if queryError := r.URL.Query().Get("error"); queryError != "" {
		http.Redirect(w, r, redirectHomeWithStatus("instagram", "cancelled"), http.StatusSeeOther)
		return
	}

	if gotState := r.URL.Query().Get("state"); gotState == "" || gotState != session.InstagramOAuthState {
		http.Redirect(w, r, redirectHomeWithStatus("instagram", "state"), http.StatusSeeOther)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Redirect(w, r, redirectHomeWithStatus("instagram", "code"), http.StatusSeeOther)
		return
	}

	token, err := instagram.ExchangeCode(code)
	if err != nil {
		log.Println("Instagram ExchangeCode error:", err)
		http.Redirect(w, r, redirectHomeWithStatus("instagram", "token"), http.StatusSeeOther)
		return
	}

	account, err := instagram.GetMyAccount(token.AccessToken)
	if err != nil {
		log.Println("Instagram GetMyAccount error:", err)
		http.Redirect(w, r, redirectHomeWithStatus("instagram", "account"), http.StatusSeeOther)
		return
	}

	session.InstagramOAuthState = ""
	session.Instagram = &appsession.InstagramConnection{
		AccessToken:       token.AccessToken,
		TokenType:         token.TokenType,
		Expiry:            token.ExpiryTime(),
		UserID:            account.ID,
		Username:          account.Username,
		ProfilePictureURL: account.ProfilePictureURL,
		FollowersCount:    account.FollowersCount,
	}
	appsession.Save(session)

	http.Redirect(w, r, redirectHomeWithStatus("instagram", "connected"), http.StatusSeeOther)
}

func instagramDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	session, ok := appsession.Get(r)
	if !ok {
		writeJSON(w, http.StatusOK, instagramStatusResponse{
			OAuthConfigured: instagram.OAuthConfigured(),
			Connected:       false,
			LiveMetrics:     []string{"FOLLOWERS", "LIKES"},
		})
		return
	}

	session.Instagram = nil
	session.InstagramOAuthState = ""
	appsession.Save(session)

	writeJSON(w, http.StatusOK, instagramStatusResponse{
		OAuthConfigured: instagram.OAuthConfigured(),
		Connected:       false,
		LiveMetrics:     []string{"FOLLOWERS", "LIKES"},
	})
}

func instagramStatus(w http.ResponseWriter, r *http.Request) {
	response := instagramStatusResponse{
		OAuthConfigured: instagram.OAuthConfigured(),
		Connected:       false,
		LiveMetrics:     []string{"FOLLOWERS", "LIKES"},
	}

	session := appsession.GetOrCreate(w, r)
	if session.Instagram != nil {
		response.Connected = true
		response.UserID = session.Instagram.UserID
		response.Username = session.Instagram.Username
		response.ProfilePictureURL = session.Instagram.ProfilePictureURL
	}

	writeJSON(w, http.StatusOK, response)
}

func instagramStats(w http.ResponseWriter, r *http.Request) {
	session, ok := appsession.Get(r)
	if !ok || session.Instagram == nil {
		writeJSON(w, http.StatusUnauthorized, instagramStatsResponse{
			instagramStatusResponse: instagramStatusResponse{
				OAuthConfigured: instagram.OAuthConfigured(),
				Connected:       false,
				LiveMetrics:     []string{"FOLLOWERS", "LIKES"},
				Error:           errInstagramNotConnected.Error(),
			},
		})
		return
	}

	updatedSession, account, likesCount, err := ensureFreshInstagramAccount(session)
	if err != nil {
		log.Println("ensureFreshInstagramAccount error:", err)
		if errors.Is(err, errInstagramNotConnected) {
			writeJSON(w, http.StatusUnauthorized, instagramStatsResponse{
				instagramStatusResponse: instagramStatusResponse{
					OAuthConfigured: instagram.OAuthConfigured(),
					Connected:       false,
					LiveMetrics:     []string{"FOLLOWERS", "LIKES"},
					Error:           err.Error(),
				},
			})
			return
		}

		writeJSON(w, http.StatusBadGateway, instagramStatsResponse{
			instagramStatusResponse: instagramStatusResponse{
				OAuthConfigured:   instagram.OAuthConfigured(),
				Connected:         updatedSession.Instagram != nil,
				UserID:            currentInstagramUserID(updatedSession),
				Username:          currentInstagramUsername(updatedSession),
				ProfilePictureURL: currentInstagramProfilePicture(updatedSession),
				LiveMetrics:       []string{"FOLLOWERS", "LIKES"},
				Error:             err.Error(),
			},
		})
		return
	}

	appsession.Save(updatedSession)
	writeJSON(w, http.StatusOK, instagramStatsResponse{
		instagramStatusResponse: instagramStatusResponse{
			OAuthConfigured:   instagram.OAuthConfigured(),
			Connected:         true,
			UserID:            account.ID,
			Username:          account.Username,
			ProfilePictureURL: account.ProfilePictureURL,
			LiveMetrics:       []string{"FOLLOWERS", "LIKES"},
		},
		FollowersCount: account.FollowersCount,
		LikeCount:      likesCount,
		FetchedAt:      time.Now().Format(time.RFC3339),
	})
}

func ensureFreshInstagramAccount(session appsession.Session) (appsession.Session, instagram.Account, string, error) {
	if session.Instagram == nil {
		return session, instagram.Account{}, "", errInstagramNotConnected
	}

	if session.Instagram.AccessToken == "" {
		session.Instagram = nil
		appsession.Save(session)
		return session, instagram.Account{}, "", errInstagramNotConnected
	}

	if time.Until(session.Instagram.Expiry) <= 24*time.Hour {
		token, err := instagram.RefreshAccessToken(session.Instagram.AccessToken)
		if err == nil {
			session.Instagram.AccessToken = token.AccessToken
			session.Instagram.TokenType = token.TokenType
			session.Instagram.Expiry = token.ExpiryTime()
		}
	}

	account, err := instagram.GetMyAccount(session.Instagram.AccessToken)
	if err != nil {
		return session, instagram.Account{}, "", err
	}

	session.Instagram.UserID = account.ID
	session.Instagram.Username = account.Username
	session.Instagram.ProfilePictureURL = account.ProfilePictureURL
	session.Instagram.FollowersCount = account.FollowersCount

	if session.Instagram.LikesCount == "" || time.Since(session.Instagram.LikesFetchedAt) >= instagramLikesCacheTTL {
		likesCount, err := instagram.GetMyLikes(session.Instagram.AccessToken)
		if err != nil {
			if session.Instagram.LikesCount != "" {
				return session, account, session.Instagram.LikesCount, nil
			}
			return session, instagram.Account{}, "", err
		}

		session.Instagram.LikesCount = likesCount
		session.Instagram.LikesFetchedAt = time.Now()
	}

	return session, account, session.Instagram.LikesCount, nil
}

func currentInstagramUserID(session appsession.Session) string {
	if session.Instagram == nil {
		return ""
	}
	return session.Instagram.UserID
}

func currentInstagramUsername(session appsession.Session) string {
	if session.Instagram == nil {
		return ""
	}
	return session.Instagram.Username
}

func currentInstagramProfilePicture(session appsession.Session) string {
	if session.Instagram == nil {
		return ""
	}
	return session.Instagram.ProfilePictureURL
}

func init() {
	if !instagram.OAuthConfigured() {
		log.Println("Instagram OAuth disabled until INSTAGRAM_CLIENT_ID, INSTAGRAM_CLIENT_SECRET, and INSTAGRAM_REDIRECT_URL are set")
	}
}
