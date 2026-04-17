package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"shikimal-sync/internal/fsx"
	"shikimal-sync/internal/model"
)

var ErrTokenNotFound = errors.New("token file does not exist")

type TokenStore struct {
	path string
	mu   sync.Mutex
}

func NewTokenStore(path string) *TokenStore {
	return &TokenStore{path: path}
}

func (s *TokenStore) Load() (*model.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrTokenNotFound
		}
		return nil, fmt.Errorf("read token file: %w", err)
	}

	var token model.Token
	if err := json.Unmarshal(raw, &token); err != nil {
		return nil, fmt.Errorf("parse token file: %w", err)
	}
	return &token, nil
}

func (s *TokenStore) Save(token *model.Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return fsx.WriteJSONAtomic(s.path, token)
}
