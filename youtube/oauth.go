package youtube

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const youtubeReadonlyScope = "https://www.googleapis.com/auth/youtube.readonly"

type oauthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type Token struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
}

type tokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func OAuthConfigured() bool {
	_, err := readOAuthConfig()
	return err == nil
}

func BuildAuthURL(state string) (string, error) {
	cfg, err := readOAuthConfig()
	if err != nil {
		return "", err
	}

	authURL, err := url.Parse("https://accounts.google.com/o/oauth2/v2/auth")
	if err != nil {
		return "", err
	}

	query := authURL.Query()
	query.Set("client_id", cfg.ClientID)
	query.Set("redirect_uri", cfg.RedirectURL)
	query.Set("response_type", "code")
	query.Set("scope", youtubeReadonlyScope)
	query.Set("access_type", "offline")
	query.Set("include_granted_scopes", "true")
	query.Set("prompt", "consent")
	query.Set("state", state)
	authURL.RawQuery = query.Encode()
	return authURL.String(), nil
}

func ExchangeCode(code string) (Token, error) {
	cfg, err := readOAuthConfig()
	if err != nil {
		return Token{}, err
	}

	form := url.Values{}
	form.Set("code", code)
	form.Set("client_id", cfg.ClientID)
	form.Set("client_secret", cfg.ClientSecret)
	form.Set("redirect_uri", cfg.RedirectURL)
	form.Set("grant_type", "authorization_code")

	return tokenRequest(form)
}

func RefreshAccessToken(refreshToken string) (Token, error) {
	cfg, err := readOAuthConfig()
	if err != nil {
		return Token{}, err
	}
	if refreshToken == "" {
		return Token{}, fmt.Errorf("refresh token is empty")
	}

	form := url.Values{}
	form.Set("client_id", cfg.ClientID)
	form.Set("client_secret", cfg.ClientSecret)
	form.Set("refresh_token", refreshToken)
	form.Set("grant_type", "refresh_token")

	return tokenRequest(form)
}

func (t Token) ExpiryTime() time.Time {
	if t.ExpiresIn <= 0 {
		return time.Now()
	}

	return time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)
}

func tokenRequest(form url.Values) (Token, error) {
	resp, err := http.Post(
		"https://oauth2.googleapis.com/token",
		"application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return Token{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Token{}, err
	}

	if resp.StatusCode >= http.StatusBadRequest {
		var tokenErr tokenErrorResponse
		if err := json.Unmarshal(body, &tokenErr); err == nil && tokenErr.Error != "" {
			return Token{}, fmt.Errorf("token exchange failed: %s (%s)", tokenErr.Error, tokenErr.ErrorDescription)
		}

		return Token{}, fmt.Errorf("token exchange failed: %s", resp.Status)
	}

	var token Token
	if err := json.Unmarshal(body, &token); err != nil {
		return Token{}, fmt.Errorf("token parse failed: %w", err)
	}

	return token, nil
}

func readOAuthConfig() (oauthConfig, error) {
	cfg := oauthConfig{
		ClientID:     strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_SECRET")),
		RedirectURL:  strings.TrimSpace(os.Getenv("YOUTUBE_REDIRECT_URL")),
	}

	var missing []string
	if cfg.ClientID == "" {
		missing = append(missing, "GOOGLE_CLIENT_ID")
	}
	if cfg.ClientSecret == "" {
		missing = append(missing, "GOOGLE_CLIENT_SECRET")
	}
	if cfg.RedirectURL == "" {
		missing = append(missing, "YOUTUBE_REDIRECT_URL")
	}

	if len(missing) > 0 {
		return oauthConfig{}, fmt.Errorf("%s must be set", strings.Join(missing, ", "))
	}

	return cfg, nil
}
