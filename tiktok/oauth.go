package tiktok

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

const (
	userInfoBasicScope   = "user.info.basic"
	userInfoProfileScope = "user.info.profile"
	userInfoStatsScope   = "user.info.stats"
	videoListScope       = "video.list"
)

type oauthConfig struct {
	ClientKey    string
	ClientSecret string
	RedirectURL  string
}

type Token struct {
	AccessToken      string `json:"access_token"`
	ExpiresIn        int64  `json:"expires_in"`
	OpenID           string `json:"open_id"`
	RefreshExpiresIn int64  `json:"refresh_expires_in"`
	RefreshToken     string `json:"refresh_token"`
	Scope            string `json:"scope"`
	TokenType        string `json:"token_type"`
}

type tokenErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	LogID            string `json:"log_id"`
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

	authURL, err := url.Parse("https://www.tiktok.com/v2/auth/authorize/")
	if err != nil {
		return "", err
	}

	query := authURL.Query()
	query.Set("client_key", cfg.ClientKey)
	query.Set("response_type", "code")
	query.Set("scope", strings.Join([]string{
		userInfoBasicScope,
		userInfoProfileScope,
		userInfoStatsScope,
		videoListScope,
	}, ","))
	query.Set("redirect_uri", cfg.RedirectURL)
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
	form.Set("client_key", cfg.ClientKey)
	form.Set("client_secret", cfg.ClientSecret)
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", cfg.RedirectURL)

	return tokenRequest(form)
}

func RefreshAccessToken(refreshToken string) (Token, error) {
	cfg, err := readOAuthConfig()
	if err != nil {
		return Token{}, err
	}
	if strings.TrimSpace(refreshToken) == "" {
		return Token{}, fmt.Errorf("refresh token is empty")
	}

	form := url.Values{}
	form.Set("client_key", cfg.ClientKey)
	form.Set("client_secret", cfg.ClientSecret)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)

	return tokenRequest(form)
}

func (t Token) ExpiryTime() time.Time {
	if t.ExpiresIn <= 0 {
		return time.Now()
	}

	return time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)
}

func (t Token) RefreshExpiryTime() time.Time {
	if t.RefreshExpiresIn <= 0 {
		return time.Time{}
	}

	return time.Now().Add(time.Duration(t.RefreshExpiresIn) * time.Second)
}

func tokenRequest(form url.Values) (Token, error) {
	resp, err := http.Post(
		"https://open.tiktokapis.com/v2/oauth/token/",
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
		ClientKey:    strings.TrimSpace(os.Getenv("TIKTOK_CLIENT_KEY")),
		ClientSecret: strings.TrimSpace(os.Getenv("TIKTOK_CLIENT_SECRET")),
		RedirectURL:  strings.TrimSpace(os.Getenv("TIKTOK_REDIRECT_URL")),
	}

	var missing []string
	if cfg.ClientKey == "" {
		missing = append(missing, "TIKTOK_CLIENT_KEY")
	}
	if cfg.ClientSecret == "" {
		missing = append(missing, "TIKTOK_CLIENT_SECRET")
	}
	if cfg.RedirectURL == "" {
		missing = append(missing, "TIKTOK_REDIRECT_URL")
	}

	if len(missing) > 0 {
		return oauthConfig{}, fmt.Errorf("%s must be set", strings.Join(missing, ", "))
	}

	return cfg, nil
}
