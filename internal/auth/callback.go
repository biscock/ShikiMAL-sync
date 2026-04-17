package auth

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"time"
)

var successTemplate = template.Must(template.New("oauth-success").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Authorization complete</title></head>
<body>
<h1>Authorization complete</h1>
<p>You can close this tab and return to the terminal.</p>
</body>
</html>`))

type callbackResult struct {
	Code  string
	State string
	Err   string
}

func WaitForCode(ctx context.Context, redirectURL string) (string, string, error) {
	u, err := url.Parse(redirectURL)
	if err != nil {
		return "", "", fmt.Errorf("parse redirect url: %w", err)
	}
	if u.Scheme != "http" {
		return "", "", errors.New("redirect url must use http for local callback")
	}

	host := u.Hostname()
	if host == "" {
		host = "127.0.0.1"
	}
	port := u.Port()
	if port == "" {
		port = "80"
	}
	addr := net.JoinHostPort(host, port)

	resultCh := make(chan callbackResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(u.Path, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		result := callbackResult{
			Code:  q.Get("code"),
			State: q.Get("state"),
			Err:   q.Get("error"),
		}

		select {
		case resultCh <- result:
		default:
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = successTemplate.Execute(w, nil)
	})

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return "", "", fmt.Errorf("listen on callback address %s: %w", addr, err)
	}

	serverErrCh := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- err
		}
	}()

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	select {
	case <-ctx.Done():
		return "", "", ctx.Err()
	case err := <-serverErrCh:
		return "", "", fmt.Errorf("callback server error: %w", err)
	case result := <-resultCh:
		if result.Err != "" {
			return "", "", fmt.Errorf("oauth error: %s", result.Err)
		}
		if result.Code == "" {
			return "", "", errors.New("authorization code is missing in callback")
		}
		return result.Code, result.State, nil
	}
}
