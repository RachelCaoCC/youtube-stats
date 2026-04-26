package board

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const defaultStorePath = "data/boards.json"

type Record struct {
	Slug            string    `json:"slug"`
	SessionID       string    `json:"sessionId"`
	PlatformKeys    []string  `json:"platformKeys"`
	TopMetricLabels []string  `json:"topMetricLabels"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type diskStore struct {
	Boards []Record `json:"boards"`
}

type store struct {
	mu        sync.RWMutex
	path      string
	bySlug    map[string]Record
	bySession map[string]string
}

var boardStore = newStore()

func init() {
	if err := boardStore.load(); err != nil {
		log.Printf("board store: %v", err)
	}
}

func GetBySession(sessionID string) (Record, bool) {
	if sessionID == "" {
		return Record{}, false
	}

	boardStore.mu.RLock()
	defer boardStore.mu.RUnlock()

	slug, ok := boardStore.bySession[sessionID]
	if !ok {
		return Record{}, false
	}

	record, ok := boardStore.bySlug[slug]
	return record, ok
}

func GetBySlug(slug string) (Record, bool) {
	if slug == "" {
		return Record{}, false
	}

	boardStore.mu.RLock()
	defer boardStore.mu.RUnlock()

	record, ok := boardStore.bySlug[slug]
	return record, ok
}

func DefaultRootSlug() string {
	boardStore.mu.RLock()
	defer boardStore.mu.RUnlock()

	if len(boardStore.bySlug) != 1 {
		return ""
	}

	for slug := range boardStore.bySlug {
		return slug
	}

	return ""
}

func UpsertForSession(sessionID string, platformKeys, topMetricLabels []string) (Record, error) {
	if sessionID == "" {
		return Record{}, errors.New("session ID is required")
	}

	boardStore.mu.Lock()
	defer boardStore.mu.Unlock()

	now := time.Now().UTC()
	record := Record{
		SessionID:       sessionID,
		PlatformKeys:    append([]string(nil), platformKeys...),
		TopMetricLabels: append([]string(nil), topMetricLabels...),
		UpdatedAt:       now,
	}

	if slug, ok := boardStore.bySession[sessionID]; ok {
		existing := boardStore.bySlug[slug]
		record.Slug = existing.Slug
		record.CreatedAt = existing.CreatedAt
	} else {
		record.Slug = boardStore.newSlugLocked()
		record.CreatedAt = now
	}

	boardStore.bySlug[record.Slug] = record
	boardStore.bySession[sessionID] = record.Slug
	if err := boardStore.persistLocked(); err != nil {
		return Record{}, err
	}

	return record, nil
}

func newStore() *store {
	return &store{
		path:      resolveStorePath(),
		bySlug:    map[string]Record{},
		bySession: map[string]string{},
	}
}

func resolveStorePath() string {
	if path := strings.TrimSpace(os.Getenv("BOARD_STORE_PATH")); path != "" {
		return path
	}

	return defaultStorePath
}

func (s *store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	payload, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var snapshot diskStore
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return err
	}

	s.bySlug = map[string]Record{}
	s.bySession = map[string]string{}
	for _, record := range snapshot.Boards {
		if record.Slug == "" || record.SessionID == "" {
			continue
		}
		s.bySlug[record.Slug] = record
		s.bySession[record.SessionID] = record.Slug
	}

	return nil
}

func (s *store) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}

	boards := make([]Record, 0, len(s.bySlug))
	for _, record := range s.bySlug {
		boards = append(boards, record)
	}

	payload, err := json.MarshalIndent(diskStore{Boards: boards}, "", "  ")
	if err != nil {
		return err
	}

	tempPath := s.path + ".tmp"
	if err := os.WriteFile(tempPath, payload, 0o600); err != nil {
		return err
	}

	return os.Rename(tempPath, s.path)
}

func (s *store) newSlugLocked() string {
	for {
		slug := "board-" + randomSlugToken(8)
		if _, exists := s.bySlug[slug]; !exists {
			return slug
		}
	}
}

func randomSlugToken(length int) string {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"

	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return strings.ToLower(strings.ReplaceAll(time.Now().UTC().Format("150405.000000"), ".", ""))
	}

	for i := range buf {
		buf[i] = alphabet[int(buf[i])%len(alphabet)]
	}

	return string(buf)
}
