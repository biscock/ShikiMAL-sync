package shikimori

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"shikimal-sync/internal/auth"
	"shikimal-sync/internal/browser"
	"shikimal-sync/internal/config"
	"shikimal-sync/internal/model"
	"shikimal-sync/internal/storage"
)

const (
	apiBase   = "https://shikimori.one/api"
	oauthBase = "https://shikimori.one/oauth"
)

type Client struct {
	httpClient *http.Client
	cfg        config.ProviderConfig
	appName    string
	tokenStore *storage.TokenStore
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

type whoamiResponse struct {
	ID int `json:"id"`
}

type userRateResponse struct {
	TargetID   int    `json:"target_id"`
	TargetType string `json:"target_type"`
	Status     string `json:"status"`
	Score      int    `json:"score"`
	Episodes   int    `json:"episodes"`
	Volumes    int    `json:"volumes"`
	Chapters   int    `json:"chapters"`
}

func NewClient(cfg config.ProviderConfig, appName string, tokenStore *storage.TokenStore) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		cfg:        cfg,
		appName:    appName,
		tokenStore: tokenStore,
	}
}

func (c *Client) Authorize(ctx context.Context) error {
	state, err := auth.RandomString(24)
	if err != nil {
		return err
	}

	query := url.Values{}
	query.Set("client_id", c.cfg.ClientID)
	query.Set("redirect_uri", c.cfg.RedirectURL)
	query.Set("response_type", "code")
	query.Set("scope", "user_rates")
	query.Set("state", state)

	authURL := oauthBase + "/authorize?" + query.Encode()
	fmt.Printf("Open this URL to authorize Shikimori:\n%s\n\n", authURL)
	if err := browser.Open(authURL); err != nil {
		fmt.Printf("Could not open a browser automatically: %v\n", err)
	}

	code, returnedState, err := auth.WaitForCode(ctx, c.cfg.RedirectURL)
	if err != nil {
		return err
	}
	if returnedState != state {
		return fmt.Errorf("oauth state mismatch")
	}

	token, err := c.exchangeCode(ctx, code)
	if err != nil {
		return err
	}
	if err := c.tokenStore.Save(token); err != nil {
		return fmt.Errorf("save shikimori token: %w", err)
	}
	fmt.Println("Shikimori token saved.")
	return nil
}

func (c *Client) CurrentUserID(ctx context.Context) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/users/whoami", nil)
	if err != nil {
		return 0, err
	}

	var resp whoamiResponse
	if err := c.doAuthorizedJSON(req, &resp); err != nil {
		return 0, err
	}
	return resp.ID, nil
}

func (c *Client) ListEntries(ctx context.Context, userID int, mediaType model.MediaType) ([]model.Entry, error) {
	query := url.Values{}
	query.Set("user_id", fmt.Sprintf("%d", userID))
	switch mediaType {
	case model.MediaTypeAnime:
		query.Set("target_type", "Anime")
	case model.MediaTypeManga:
		query.Set("target_type", "Manga")
	default:
		return nil, fmt.Errorf("unsupported media type: %s", mediaType)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/v2/user_rates?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}

	var payload []userRateResponse
	if err := c.doAuthorizedJSON(req, &payload); err != nil {
		return nil, err
	}

	entries := make([]model.Entry, 0, len(payload))
	for _, item := range payload {
		entry := model.Entry{
			ID:        item.TargetID,
			MediaType: mediaType,
			Status:    item.Status,
			Score:     item.Score,
			Episodes:  item.Episodes,
			Chapters:  item.Chapters,
			Volumes:   item.Volumes,
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (c *Client) exchangeCode(ctx context.Context, code string) (*model.Token, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_secret", c.cfg.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", c.cfg.RedirectURL)

	return c.exchangeToken(ctx, form)
}

func (c *Client) refreshToken(ctx context.Context, refreshToken string) (*model.Token, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_secret", c.cfg.ClientSecret)
	form.Set("refresh_token", refreshToken)

	return c.exchangeToken(ctx, form)
}

func (c *Client) exchangeToken(ctx context.Context, form url.Values) (*model.Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, oauthBase+"/token", bytes.NewBufferString(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", c.appName)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("token endpoint returned %s: %s", resp.Status, string(body))
	}

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}

	expiresAt := time.Now().UTC().Add(24 * time.Hour)
	if tr.ExpiresIn > 0 {
		expiresAt = time.Now().UTC().Add(time.Duration(tr.ExpiresIn) * time.Second)
	}

	return &model.Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		TokenType:    tr.TokenType,
		ExpiresAt:    expiresAt,
	}, nil
}

func (c *Client) doAuthorizedJSON(req *http.Request, target any) error {
	resp, err := c.doAuthorized(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("shikimori request failed with %s: %s", resp.Status, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode shikimori response: %w", err)
	}
	return nil
}

func (c *Client) doAuthorized(req *http.Request) (*http.Response, error) {
	token, err := c.ensureFreshToken(req.Context())
	if err != nil {
		return nil, err
	}

	cloned, err := cloneRequest(req)
	if err != nil {
		return nil, err
	}
	cloned.Header.Set("Authorization", "Bearer "+token.AccessToken)
	cloned.Header.Set("User-Agent", c.appName)
	cloned.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(cloned)
	if err != nil {
		return nil, fmt.Errorf("request shikimori: %w", err)
	}

	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	resp.Body.Close()

	if token.RefreshToken == "" {
		return nil, fmt.Errorf("shikimori token expired and no refresh token is available")
	}

	refreshed, err := c.refreshToken(req.Context(), token.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("refresh shikimori token: %w", err)
	}
	if err := c.tokenStore.Save(refreshed); err != nil {
		return nil, err
	}

	retry, err := cloneRequest(req)
	if err != nil {
		return nil, err
	}
	retry.Header.Set("Authorization", "Bearer "+refreshed.AccessToken)
	retry.Header.Set("User-Agent", c.appName)
	retry.Header.Set("Accept", "application/json")
	return c.httpClient.Do(retry)
}

func (c *Client) ensureFreshToken(ctx context.Context) (*model.Token, error) {
	token, err := c.tokenStore.Load()
	if err != nil {
		if err == storage.ErrTokenNotFound {
			return nil, fmt.Errorf("shikimori token missing, run auth-shiki first")
		}
		return nil, err
	}

	if token.ExpiresAt.IsZero() || time.Until(token.ExpiresAt) > 30*time.Second {
		return token, nil
	}
	if token.RefreshToken == "" {
		return token, nil
	}

	refreshed, err := c.refreshToken(ctx, token.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("refresh shikimori token: %w", err)
	}
	if err := c.tokenStore.Save(refreshed); err != nil {
		return nil, err
	}
	return refreshed, nil
}

func cloneRequest(req *http.Request) (*http.Request, error) {
	cloned := req.Clone(req.Context())
	if req.Body != nil && req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, fmt.Errorf("clone request body: %w", err)
		}
		cloned.Body = body
	}
	return cloned, nil
}
