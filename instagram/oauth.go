package instagram

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
	instagramBusinessBasicScope          = "instagram_business_basic"
	instagramBusinessManageInsightsScope = "instagram_business_manage_insights"
)

type oauthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type Token struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	UserID      int64  `json:"user_id,omitempty"`
}

type shortLivedToken struct {
	AccessToken string `json:"access_token"`
	UserID      int64  `json:"user_id,omitempty"`
}

type tokenErrorResponse struct {
	ErrorType        string `json:"error_type"`
	Code             int    `json:"code"`
	ErrorMessage     string `json:"error_message"`
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

	authURL, err := url.Parse("https://www.instagram.com/oauth/authorize")
	if err != nil {
		return "", err
	}

	query := authURL.Query()
	query.Set("client_id", cfg.ClientID)
	query.Set("redirect_uri", cfg.RedirectURL)
	query.Set("response_type", "code")
	query.Set("scope", strings.Join([]string{
		instagramBusinessBasicScope,
		instagramBusinessManageInsightsScope,
	}, ","))
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
	form.Set("client_id", cfg.ClientID)
	form.Set("client_secret", cfg.ClientSecret)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", cfg.RedirectURL)
	form.Set("code", code)

	resp, err := http.Post(
		"https://api.instagram.com/oauth/access_token",
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
		return Token{}, parseTokenError(body, resp.Status)
	}

	var shortToken shortLivedToken
	if err := json.Unmarshal(body, &shortToken); err != nil {
		return Token{}, fmt.Errorf("instagram token parse failed: %w", err)
	}

	longToken, err := exchangeLongLivedToken(shortToken.AccessToken, shortToken.UserID)
	if err != nil {
		return Token{}, err
	}

	return longToken, nil
}

func RefreshAccessToken(accessToken string) (Token, error) {
	if strings.TrimSpace(accessToken) == "" {
		return Token{}, fmt.Errorf("instagram access token is empty")
	}

	req, err := http.NewRequest("GET", "https://graph.instagram.com/refresh_access_token", nil)
	if err != nil {
		return Token{}, err
	}

	query := req.URL.Query()
	query.Set("grant_type", "ig_refresh_token")
	query.Set("access_token", accessToken)
	req.URL.RawQuery = query.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Token{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Token{}, err
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return Token{}, parseTokenError(body, resp.Status)
	}

	var token Token
	if err := json.Unmarshal(body, &token); err != nil {
		return Token{}, fmt.Errorf("instagram refresh token parse failed: %w", err)
	}

	if token.AccessToken == "" {
		token.AccessToken = accessToken
	}

	return token, nil
}

func (t Token) ExpiryTime() time.Time {
	if t.ExpiresIn <= 0 {
		return time.Now()
	}

	return time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)
}

func exchangeLongLivedToken(accessToken string, userID int64) (Token, error) {
	cfg, err := readOAuthConfig()
	if err != nil {
		return Token{}, err
	}

	req, err := http.NewRequest("GET", "https://graph.instagram.com/access_token", nil)
	if err != nil {
		return Token{}, err
	}

	query := req.URL.Query()
	query.Set("grant_type", "ig_exchange_token")
	query.Set("client_secret", cfg.ClientSecret)
	query.Set("access_token", accessToken)
	req.URL.RawQuery = query.Encode()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Token{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Token{}, err
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return Token{}, parseTokenError(body, resp.Status)
	}

	var token Token
	if err := json.Unmarshal(body, &token); err != nil {
		return Token{}, fmt.Errorf("instagram long-lived token parse failed: %w", err)
	}

	token.UserID = userID
	return token, nil
}

func parseTokenError(body []byte, status string) error {
	var tokenErr tokenErrorResponse
	if err := json.Unmarshal(body, &tokenErr); err == nil {
		switch {
		case tokenErr.ErrorMessage != "":
			return fmt.Errorf("instagram token request failed: %s", tokenErr.ErrorMessage)
		case tokenErr.ErrorDescription != "":
			return fmt.Errorf("instagram token request failed: %s", tokenErr.ErrorDescription)
		}
	}

	return fmt.Errorf("instagram token request failed: %s", status)
}

func readOAuthConfig() (oauthConfig, error) {
	cfg := oauthConfig{
		ClientID:     strings.TrimSpace(os.Getenv("INSTAGRAM_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("INSTAGRAM_CLIENT_SECRET")),
		RedirectURL:  strings.TrimSpace(os.Getenv("INSTAGRAM_REDIRECT_URL")),
	}

	var missing []string
	if cfg.ClientID == "" {
		missing = append(missing, "INSTAGRAM_CLIENT_ID")
	}
	if cfg.ClientSecret == "" {
		missing = append(missing, "INSTAGRAM_CLIENT_SECRET")
	}
	if cfg.RedirectURL == "" {
		missing = append(missing, "INSTAGRAM_REDIRECT_URL")
	}

	if len(missing) > 0 {
		return oauthConfig{}, fmt.Errorf("%s must be set", strings.Join(missing, ", "))
	}

	return cfg, nil
}
