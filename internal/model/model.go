package model

import "time"

type MediaType string

const (
	MediaTypeAnime MediaType = "anime"
	MediaTypeManga MediaType = "manga"
)

type Entry struct {
	ID        int       `json:"id"`
	MediaType MediaType `json:"media_type"`
	Status    string    `json:"status"`
	Score     int       `json:"score"`
	Episodes  int       `json:"episodes,omitempty"`
	Chapters  int       `json:"chapters,omitempty"`
	Volumes   int       `json:"volumes,omitempty"`
}

func (e Entry) Key() string {
	return string(e.MediaType) + ":" + itoa(e.ID)
}

type Snapshot struct {
	Version    int              `json:"version"`
	CapturedAt time.Time        `json:"captured_at"`
	Entries    map[string]Entry `json:"entries"`
}

type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}

	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}

	buf := [20]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return sign + string(buf[i:])
}
