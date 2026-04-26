package appsession

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	cookieName             = "youtube_stats_session"
	sessionLifetime        = 30 * 24 * time.Hour
	defaultStorePath       = "data/sessions.json"
	encryptionStoreVersion = 1
)

type Session struct {
	ID                  string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	ExpiresAt           time.Time
	Pinned              bool
	YouTubeOAuthState   string
	InstagramOAuthState string
	TikTokOAuthState    string
	YouTube             *YouTubeConnection
	Instagram           *InstagramConnection
	TikTok              *TikTokConnection
}

type YouTubeConnection struct {
	AccessToken     string
	RefreshToken    string
	TokenType       string
	Expiry          time.Time
	ChannelID       string
	ChannelTitle    string
	ChannelThumbURL string
	LikesCount      string
	LikesFetchedAt  time.Time
}

type InstagramConnection struct {
	AccessToken       string
	TokenType         string
	Expiry            time.Time
	UserID            string
	Username          string
	ProfilePictureURL string
	FollowersCount    string
	LikesCount        string
	LikesFetchedAt    time.Time
}

type TikTokConnection struct {
	AccessToken    string
	RefreshToken   string
	TokenType      string
	Expiry         time.Time
	RefreshExpiry  time.Time
	OpenID         string
	DisplayName    string
	Username       string
	AvatarURL      string
	FollowersCount string
	LikesCount     string
	ViewsCount     string
	ViewsFetchedAt time.Time
}

type store struct {
	mu       sync.RWMutex
	path     string
	cipher   *storeCipher
	sessions map[string]Session
}

type diskStore struct {
	Sessions map[string]Session `json:"sessions"`
}

type encryptedStoreEnvelope struct {
	Version    int    `json:"version"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type storeCipher struct {
	aead cipher.AEAD
}

var (
	errEncryptionKeyRequired = errors.New("SESSION_ENCRYPTION_KEY is required to read the encrypted session store")
	sessionStore             = newStore()
)

func init() {
	if err := sessionStore.load(); err != nil {
		log.Printf("session store: %v", err)
	}
	if sessionStore.cipher == nil {
		log.Printf("session store: SESSION_ENCRYPTION_KEY not set; persisted OAuth sessions are stored unencrypted")
	}
}

func Get(r *http.Request) (Session, bool) {
	cookie, err := r.Cookie(cookieName)
	if err != nil || cookie.Value == "" {
		return Session{}, false
	}

	return sessionStore.get(cookie.Value)
}

func GetByID(sessionID string) (Session, bool) {
	if sessionID == "" {
		return Session{}, false
	}

	session, ok := sessionStore.get(sessionID)
	if !ok {
		return Session{}, false
	}

	return sessionStore.touch(session), true
}

func GetOrCreate(w http.ResponseWriter, r *http.Request) Session {
	if session, ok := Get(r); ok {
		session = sessionStore.touch(session)
		setCookie(w, r, session.ID)
		return session
	}

	id := randomToken()
	now := time.Now().UTC()
	session := Session{
		ID:        id,
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: now.Add(sessionLifetime),
	}
	Save(session)
	setCookie(w, r, id)
	return session
}

func Save(session Session) {
	sessionStore.mu.Lock()
	sessionStore.saveLocked(session)
	sessionStore.mu.Unlock()
}

func NewStateToken() string {
	return randomToken()
}

func Delete(sessionID string) {
	if sessionID == "" {
		return
	}

	sessionStore.mu.Lock()
	delete(sessionStore.sessions, sessionID)
	sessionStore.persistLocked()
	sessionStore.mu.Unlock()
}

func Pin(sessionID string) bool {
	if sessionID == "" {
		return false
	}

	sessionStore.mu.Lock()
	defer sessionStore.mu.Unlock()

	session, ok := sessionStore.sessions[sessionID]
	if !ok {
		return false
	}

	session.Pinned = true
	sessionStore.saveLocked(session)
	return true
}

func setCookie(w http.ResponseWriter, r *http.Request, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPS(r),
		MaxAge:   int(sessionLifetime / time.Second),
	})
}

func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}

	return r.Header.Get("X-Forwarded-Proto") == "https"
}

func (s *store) get(sessionID string) (Session, bool) {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if !ok {
		return Session{}, false
	}

	if session.isExpired() {
		Delete(sessionID)
		return Session{}, false
	}

	return session, true
}

func (s *store) touch(session Session) Session {
	s.mu.Lock()
	session = s.normalizeSession(session)
	session.UpdatedAt = time.Now().UTC()
	session.ExpiresAt = session.UpdatedAt.Add(sessionLifetime)
	s.sessions[session.ID] = session
	s.persistLocked()
	s.mu.Unlock()
	return session
}

func (s *store) saveLocked(session Session) {
	session = s.normalizeSession(session)
	s.sessions[session.ID] = session
	s.persistLocked()
}

func (s *store) normalizeSession(session Session) Session {
	now := time.Now().UTC()
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	session.UpdatedAt = now
	if session.ExpiresAt.IsZero() || session.ExpiresAt.Before(now) {
		session.ExpiresAt = now.Add(sessionLifetime)
	}
	return session
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

	decodedPayload, migratePlaintext, err := s.decodePayload(payload)
	if err != nil {
		return err
	}

	var snapshot diskStore
	if err := json.Unmarshal(decodedPayload, &snapshot); err != nil {
		return err
	}
	if snapshot.Sessions == nil {
		s.sessions = map[string]Session{}
		return nil
	}

	s.sessions = snapshot.Sessions
	s.pruneExpiredLocked()
	if migratePlaintext {
		s.persistLocked()
	}
	return nil
}

func (s *store) persistLocked() {
	s.pruneExpiredLocked()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		log.Printf("session store: create dir: %v", err)
		return
	}

	payload, err := json.MarshalIndent(diskStore{Sessions: s.sessions}, "", "  ")
	if err != nil {
		log.Printf("session store: marshal: %v", err)
		return
	}

	if s.cipher != nil {
		payload, err = s.cipher.encrypt(payload)
		if err != nil {
			log.Printf("session store: encrypt: %v", err)
			return
		}
	}

	tempPath := s.path + ".tmp"
	if err := os.WriteFile(tempPath, payload, 0o600); err != nil {
		log.Printf("session store: write temp file: %v", err)
		return
	}

	if err := os.Rename(tempPath, s.path); err != nil {
		log.Printf("session store: rename temp file: %v", err)
	}
}

func (s *store) pruneExpiredLocked() {
	if len(s.sessions) == 0 {
		return
	}

	for id, session := range s.sessions {
		if session.isExpired() {
			delete(s.sessions, id)
		}
	}
}

func (s Session) isExpired() bool {
	if s.Pinned {
		return false
	}

	if s.ExpiresAt.IsZero() {
		return false
	}

	return time.Now().UTC().After(s.ExpiresAt)
}

func randomToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return base64.RawURLEncoding.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}

	return base64.RawURLEncoding.EncodeToString(buf)
}

func resolveStorePath() string {
	if path := os.Getenv("SESSION_STORE_PATH"); path != "" {
		return path
	}

	return defaultStorePath
}

func newStore() store {
	store := store{
		path:     resolveStorePath(),
		sessions: map[string]Session{},
	}

	sessionCipher, err := resolveStoreCipher()
	if err != nil {
		log.Printf("session store: %v", err)
		return store
	}

	store.cipher = sessionCipher
	return store
}

func resolveStoreCipher() (*storeCipher, error) {
	keyValue := strings.TrimSpace(os.Getenv("SESSION_ENCRYPTION_KEY"))
	if keyValue == "" {
		return nil, nil
	}

	return resolveStoreCipherFromValue(keyValue)
}

func resolveStoreCipherFromValue(keyValue string) (*storeCipher, error) {
	keyValue = strings.TrimSpace(keyValue)
	if keyValue == "" {
		return nil, nil
	}

	key, err := decodeStoreKey(keyValue)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("build encryption cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build encryption GCM: %w", err)
	}

	return &storeCipher{aead: aead}, nil
}

func decodeStoreKey(keyValue string) ([]byte, error) {
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}

	lengthMismatch := false
	for _, encoding := range encodings {
		key, err := encoding.DecodeString(keyValue)
		if err == nil {
			if len(key) == 32 {
				return key, nil
			}
			lengthMismatch = true
		}
	}

	if len(keyValue) == 32 {
		return []byte(keyValue), nil
	}

	if lengthMismatch {
		return nil, errors.New("SESSION_ENCRYPTION_KEY must decode to exactly 32 bytes")
	}

	return nil, errors.New("SESSION_ENCRYPTION_KEY must be a 32-byte raw string or base64-encoded 32-byte key")
}

func (s *store) decodePayload(payload []byte) ([]byte, bool, error) {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return []byte(`{"sessions":{}}`), false, nil
	}

	var envelope encryptedStoreEnvelope
	if err := json.Unmarshal(trimmed, &envelope); err == nil && envelope.isEncrypted() {
		if s.cipher == nil {
			return nil, false, errEncryptionKeyRequired
		}

		plaintext, err := s.cipher.decrypt(envelope)
		if err != nil {
			return nil, false, err
		}

		return plaintext, false, nil
	}

	return trimmed, s.cipher != nil, nil
}

func (e encryptedStoreEnvelope) isEncrypted() bool {
	return e.Version != 0 || e.Nonce != "" || e.Ciphertext != ""
}

func (c *storeCipher) encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate encryption nonce: %w", err)
	}

	ciphertext := c.aead.Seal(nil, nonce, plaintext, []byte("youtube-stats/session-store/v1"))
	envelope := encryptedStoreEnvelope{
		Version:    encryptionStoreVersion,
		Nonce:      base64.RawURLEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawURLEncoding.EncodeToString(ciphertext),
	}

	encoded, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal encrypted session store: %w", err)
	}

	return encoded, nil
}

func (c *storeCipher) decrypt(envelope encryptedStoreEnvelope) ([]byte, error) {
	if envelope.Version != encryptionStoreVersion {
		return nil, fmt.Errorf("unsupported encrypted session store version %d", envelope.Version)
	}

	nonce, err := base64.RawURLEncoding.DecodeString(envelope.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decode session nonce: %w", err)
	}

	ciphertext, err := base64.RawURLEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode session ciphertext: %w", err)
	}

	plaintext, err := c.aead.Open(nil, nonce, ciphertext, []byte("youtube-stats/session-store/v1"))
	if err != nil {
		return nil, fmt.Errorf("decrypt session store: %w", err)
	}

	return plaintext, nil
}
