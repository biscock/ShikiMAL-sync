package syncer

import (
	"context"
	"testing"

	"shikimal-sync/internal/model"
)

type fakeShikimori struct {
	userID  int
	entries map[model.MediaType][]model.Entry
}

func (f *fakeShikimori) CurrentUserID(context.Context) (int, error) {
	return f.userID, nil
}

func (f *fakeShikimori) ListEntries(_ context.Context, _ int, mediaType model.MediaType) ([]model.Entry, error) {
	return f.entries[mediaType], nil
}

type fakeMAL struct {
	upserts []model.Entry
	deletes []string
}

func (f *fakeMAL) UpsertEntry(_ context.Context, entry model.Entry) error {
	f.upserts = append(f.upserts, entry)
	return nil
}

func (f *fakeMAL) DeleteEntry(_ context.Context, mediaType model.MediaType, id int) error {
	f.deletes = append(f.deletes, string(mediaType)+":"+modelEntryID(id))
	return nil
}

func TestApplyDiffCountsUpdatesAndDeletes(t *testing.T) {
	engine := &Engine{}
	baseline := &model.Snapshot{
		Entries: map[string]model.Entry{
			"anime:1": {ID: 1, MediaType: model.MediaTypeAnime, Status: "watching", Episodes: 1},
			"manga:2": {ID: 2, MediaType: model.MediaTypeManga, Status: "reading", Chapters: 3},
		},
	}
	current := &model.Snapshot{
		Entries: map[string]model.Entry{
			"anime:1": {ID: 1, MediaType: model.MediaTypeAnime, Status: "watching", Episodes: 2},
		},
	}
	mal := &fakeMAL{}
	engine.mal = mal
	engine.state = nilStateStore{}

	result, err := engine.applyDiff(context.Background(), baseline, current)
	if err != nil {
		t.Fatalf("applyDiff returned error: %v", err)
	}
	if result.Updated != 1 {
		t.Fatalf("expected 1 update, got %d", result.Updated)
	}
	if result.Deleted != 1 {
		t.Fatalf("expected 1 delete, got %d", result.Deleted)
	}
	if len(mal.upserts) != 1 || mal.upserts[0].Episodes != 2 {
		t.Fatalf("unexpected upserts: %#v", mal.upserts)
	}
	if len(mal.deletes) != 1 || mal.deletes[0] != "manga:2" {
		t.Fatalf("unexpected deletes: %#v", mal.deletes)
	}
}

type nilStateStore struct{}

func (nilStateStore) Load() (*model.Snapshot, error) { return nil, nil }
func (nilStateStore) Save(*model.Snapshot) error     { return nil }

func modelEntryID(id int) string {
	if id == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	for id > 0 {
		i--
		buf[i] = byte('0' + id%10)
		id /= 10
	}
	return string(buf[i:])
}
