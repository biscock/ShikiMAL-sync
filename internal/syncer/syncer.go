package syncer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"shikimal-sync/internal/model"
	"shikimal-sync/internal/storage"
)

const (
	stateVersion = 1
	writeDelay   = 400 * time.Millisecond
)

type ShikimoriClient interface {
	CurrentUserID(ctx context.Context) (int, error)
	ListEntries(ctx context.Context, userID int, mediaType model.MediaType) ([]model.Entry, error)
}

type MALClient interface {
	UpsertEntry(ctx context.Context, entry model.Entry) error
	DeleteEntry(ctx context.Context, mediaType model.MediaType, id int) error
}

type StateStore interface {
	Load() (*model.Snapshot, error)
	Save(snapshot *model.Snapshot) error
}

type Engine struct {
	shikimori ShikimoriClient
	mal       MALClient
	state     StateStore
}

type CycleResult struct {
	Baselined int
	Updated   int
	Deleted   int
}

func NewEngine(shiki ShikimoriClient, mal MALClient, state StateStore) *Engine {
	return &Engine{
		shikimori: shiki,
		mal:       mal,
		state:     state,
	}
}

func (e *Engine) RunOnce(ctx context.Context) (CycleResult, error) {
	userID, err := e.shikimori.CurrentUserID(ctx)
	if err != nil {
		return CycleResult{}, fmt.Errorf("get shikimori user id: %w", err)
	}

	current, err := e.fetchSnapshot(ctx, userID)
	if err != nil {
		return CycleResult{}, err
	}

	baseline, err := e.state.Load()
	if err != nil {
		if errors.Is(err, storage.ErrStateNotFound) {
			if err := e.state.Save(current); err != nil {
				return CycleResult{}, fmt.Errorf("save initial baseline: %w", err)
			}
			return CycleResult{Baselined: len(current.Entries)}, nil
		}
		return CycleResult{}, err
	}

	result, err := e.applyDiff(ctx, baseline, current)
	if err != nil {
		return result, err
	}

	return result, nil
}

func (e *Engine) fetchSnapshot(ctx context.Context, userID int) (*model.Snapshot, error) {
	anime, err := e.shikimori.ListEntries(ctx, userID, model.MediaTypeAnime)
	if err != nil {
		return nil, fmt.Errorf("fetch anime entries: %w", err)
	}
	manga, err := e.shikimori.ListEntries(ctx, userID, model.MediaTypeManga)
	if err != nil {
		return nil, fmt.Errorf("fetch manga entries: %w", err)
	}

	entries := make(map[string]model.Entry, len(anime)+len(manga))
	for _, entry := range anime {
		entries[entry.Key()] = entry
	}
	for _, entry := range manga {
		entries[entry.Key()] = entry
	}

	return &model.Snapshot{
		Version:    stateVersion,
		CapturedAt: time.Now().UTC(),
		Entries:    entries,
	}, nil
}

func (e *Engine) applyDiff(ctx context.Context, baseline, current *model.Snapshot) (CycleResult, error) {
	result := CycleResult{}

	for key, entry := range current.Entries {
		oldEntry, exists := baseline.Entries[key]
		if exists && oldEntry == entry {
			continue
		}

		if err := e.mal.UpsertEntry(ctx, entry); err != nil {
			return result, fmt.Errorf("sync entry %s: %w", key, err)
		}
		baseline.Entries[key] = entry
		baseline.CapturedAt = current.CapturedAt
		if err := e.state.Save(baseline); err != nil {
			return result, fmt.Errorf("save state after upsert %s: %w", key, err)
		}
		result.Updated++
		if err := sleepContext(ctx, writeDelay); err != nil {
			return result, err
		}
	}

	for key, oldEntry := range baseline.Entries {
		if _, exists := current.Entries[key]; exists {
			continue
		}

		if err := e.mal.DeleteEntry(ctx, oldEntry.MediaType, oldEntry.ID); err != nil {
			return result, fmt.Errorf("delete entry %s: %w", key, err)
		}
		delete(baseline.Entries, key)
		baseline.CapturedAt = current.CapturedAt
		if err := e.state.Save(baseline); err != nil {
			return result, fmt.Errorf("save state after delete %s: %w", key, err)
		}
		result.Deleted++
		if err := sleepContext(ctx, writeDelay); err != nil {
			return result, err
		}
	}

	if result.Updated == 0 && result.Deleted == 0 {
		baseline.CapturedAt = current.CapturedAt
		if err := e.state.Save(baseline); err != nil {
			return result, fmt.Errorf("save state timestamp: %w", err)
		}
	}
	return result, nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
