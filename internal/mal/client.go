package mal

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
	apiBase   = "https://api.myanimelist.net/v2"
	authBase  = "https://myanimelist.net/v1/oauth2"
	retrySkew = 30 * time.Second
)

type Client struct {
	httpClient *http.Client
	cfg        config.ProviderConfig
	tokenStore *storage.TokenStore
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func NewClient(cfg config.ProviderConfig, tokenStore *storage.TokenStore) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		cfg:        cfg,
		tokenStore: tokenStore,
	}
}

func (c *Client) Authorize(ctx context.Context) error {
	state, err := auth.RandomString(24)
	if err != nil {
		return err
	}
	verifier, err := auth.RandomString(64)
	if err != nil {
		return err
	}
	challenge := verifier

	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", c.cfg.ClientID)
	query.Set("redirect_uri", c.cfg.RedirectURL)
	query.Set("state", state)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "plain")

	authURL := authBase + "/authorize?" + query.Encode()
	fmt.Printf("Open this URL to authorize MyAnimeList:\n%s\n\n", authURL)
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

	token, err := c.exchangeCode(ctx, code, verifier)
	if err != nil {
		return err
	}
	if err := c.tokenStore.Save(token); err != nil {
		return fmt.Errorf("save MAL token: %w", err)
	}
	fmt.Println("MyAnimeList token saved.")
	return nil
}

func (c *Client) UpsertEntry(ctx context.Context, entry model.Entry) error {
	form := url.Values{}
	status, err := mapStatus(entry)
	if err != nil {
		return err
	}
	form.Set("status", status)
	form.Set("score", fmt.Sprintf("%d", entry.Score))

	path := ""
	switch entry.MediaType {
	case model.MediaTypeAnime:
		form.Set("num_watched_episodes", fmt.Sprintf("%d", entry.Episodes))
		path = fmt.Sprintf("%s/anime/%d/my_list_status", apiBase, entry.ID)
	case model.MediaTypeManga:
		form.Set("num_chapters_read", fmt.Sprintf("%d", entry.Chapters))
		form.Set("num_volumes_read", fmt.Sprintf("%d", entry.Volumes))
		path = fmt.Sprintf("%s/manga/%d/my_list_status", apiBase, entry.ID)
	default:
		return fmt.Errorf("unsupported media type: %s", entry.MediaType)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, path, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.doAuthorized(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("MAL upsert failed for %s %d with %s: %s", entry.MediaType, entry.ID, resp.Status, string(body))
	}
	return nil
}

func (c *Client) DeleteEntry(ctx context.Context, mediaType model.MediaType, id int) error {
	var path string
	switch mediaType {
	case model.MediaTypeAnime:
		path = fmt.Sprintf("%s/anime/%d/my_list_status", apiBase, id)
	case model.MediaTypeManga:
		path = fmt.Sprintf("%s/manga/%d/my_list_status", apiBase, id)
	default:
		return fmt.Errorf("unsupported media type: %s", mediaType)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}

	resp, err := c.doAuthorized(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("MAL delete failed for %s %d with %s: %s", mediaType, id, resp.Status, string(body))
	}
	return nil
}

func (c *Client) exchangeCode(ctx context.Context, code, verifier string) (*model.Token, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", c.cfg.ClientID)
	if c.cfg.ClientSecret != "" {
		form.Set("client_secret", c.cfg.ClientSecret)
	}
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("redirect_uri", c.cfg.RedirectURL)
	return c.exchangeToken(ctx, form)
}

func (c *Client) refreshToken(ctx context.Context, refreshToken string) (*model.Token, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", c.cfg.ClientID)
	if c.cfg.ClientSecret != "" {
		form.Set("client_secret", c.cfg.ClientSecret)
	}
	form.Set("refresh_token", refreshToken)
	return c.exchangeToken(ctx, form)
}

func (c *Client) exchangeToken(ctx context.Context, form url.Values) (*model.Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authBase+"/token", bytes.NewBufferString(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request MAL token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("MAL token endpoint returned %s: %s", resp.Status, string(body))
	}

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("decode MAL token response: %w", err)
	}

	expiresAt := time.Now().UTC()
	if tr.ExpiresIn > 0 {
		expiresAt = expiresAt.Add(time.Duration(tr.ExpiresIn) * time.Second)
	}

	return &model.Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		TokenType:    tr.TokenType,
		ExpiresAt:    expiresAt,
	}, nil
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
	cloned.Header.Set("X-MAL-CLIENT-ID", c.cfg.ClientID)
	cloned.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(cloned)
	if err != nil {
		return nil, fmt.Errorf("request MAL API: %w", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	resp.Body.Close()

	if token.RefreshToken == "" {
		return nil, fmt.Errorf("MAL token expired and no refresh token is available")
	}

	refreshed, err := c.refreshToken(req.Context(), token.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("refresh MAL token: %w", err)
	}
	if err := c.tokenStore.Save(refreshed); err != nil {
		return nil, err
	}

	retry, err := cloneRequest(req)
	if err != nil {
		return nil, err
	}
	retry.Header.Set("Authorization", "Bearer "+refreshed.AccessToken)
	retry.Header.Set("X-MAL-CLIENT-ID", c.cfg.ClientID)
	retry.Header.Set("Accept", "application/json")
	return c.httpClient.Do(retry)
}

func (c *Client) ensureFreshToken(ctx context.Context) (*model.Token, error) {
	token, err := c.tokenStore.Load()
	if err != nil {
		if err == storage.ErrTokenNotFound {
			return nil, fmt.Errorf("MAL token missing, run auth-mal first")
		}
		return nil, err
	}

	if token.ExpiresAt.IsZero() || time.Until(token.ExpiresAt) > retrySkew {
		return token, nil
	}
	if token.RefreshToken == "" {
		return token, nil
	}

	refreshed, err := c.refreshToken(ctx, token.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("refresh MAL token: %w", err)
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

func mapStatus(entry model.Entry) (string, error) {
	switch entry.MediaType {
	case model.MediaTypeAnime:
		switch entry.Status {
		case "planned":
			return "plan_to_watch", nil
		case "watching", "rewatching":
			return "watching", nil
		case "completed":
			return "completed", nil
		case "on_hold":
			return "on_hold", nil
		case "dropped":
			return "dropped", nil
		}
	case model.MediaTypeManga:
		switch entry.Status {
		case "planned":
			return "plan_to_read", nil
		case "watching", "rewatching":
			return "reading", nil
		case "completed":
			return "completed", nil
		case "on_hold":
			return "on_hold", nil
		case "dropped":
			return "dropped", nil
		}
	}
	return "", fmt.Errorf("unsupported status %q for %s", entry.Status, entry.MediaType)
}
