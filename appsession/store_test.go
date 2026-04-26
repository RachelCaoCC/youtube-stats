package appsession

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEncryptedStoreRoundTrip(t *testing.T) {
	t.Parallel()

	sessionCipher, err := resolveStoreCipherForTest()
	if err != nil {
		t.Fatalf("resolveStoreCipherForTest: %v", err)
	}

	storePath := filepath.Join(t.TempDir(), "sessions.json")
	session := Session{
		ID:        "session-1",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(sessionLifetime),
		YouTube: &YouTubeConnection{
			AccessToken:  "youtube-access-token",
			RefreshToken: "youtube-refresh-token",
		},
	}

	sessionStore := store{
		path:     storePath,
		cipher:   sessionCipher,
		sessions: map[string]Session{session.ID: session},
	}

	sessionStore.persistLocked()

	payload, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	if strings.Contains(string(payload), session.YouTube.AccessToken) {
		t.Fatalf("encrypted store leaked access token in plaintext")
	}

	loadedStore := store{
		path:     storePath,
		cipher:   sessionCipher,
		sessions: map[string]Session{},
	}
	if err := loadedStore.load(); err != nil {
		t.Fatalf("load: %v", err)
	}

	loadedSession, ok := loadedStore.sessions[session.ID]
	if !ok {
		t.Fatalf("expected session %q after load", session.ID)
	}
	if loadedSession.YouTube == nil || loadedSession.YouTube.AccessToken != session.YouTube.AccessToken {
		t.Fatalf("expected encrypted session tokens to round-trip")
	}
}

func TestPlaintextStoreMigratesToEncryptedWhenCipherConfigured(t *testing.T) {
	t.Parallel()

	sessionCipher, err := resolveStoreCipherForTest()
	if err != nil {
		t.Fatalf("resolveStoreCipherForTest: %v", err)
	}

	storePath := filepath.Join(t.TempDir(), "sessions.json")
	session := Session{
		ID:        "session-2",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(sessionLifetime),
		Instagram: &InstagramConnection{
			AccessToken: "instagram-access-token",
			TokenType:   "Bearer",
		},
	}

	plaintextPayload, err := json.MarshalIndent(diskStore{
		Sessions: map[string]Session{session.ID: session},
	}, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	if err := os.WriteFile(storePath, plaintextPayload, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	loadedStore := store{
		path:     storePath,
		cipher:   sessionCipher,
		sessions: map[string]Session{},
	}
	if err := loadedStore.load(); err != nil {
		t.Fatalf("load: %v", err)
	}

	migratedPayload, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(migratedPayload), session.Instagram.AccessToken) {
		t.Fatalf("expected plaintext session store to be migrated to encrypted format")
	}

	var envelope encryptedStoreEnvelope
	if err := json.Unmarshal(migratedPayload, &envelope); err != nil {
		t.Fatalf("Unmarshal encrypted envelope: %v", err)
	}
	if !envelope.isEncrypted() {
		t.Fatalf("expected migrated payload to be encrypted envelope")
	}
}

func TestPinnedSessionDoesNotExpire(t *testing.T) {
	t.Parallel()

	session := Session{
		ID:        "session-3",
		Pinned:    true,
		ExpiresAt: time.Now().UTC().Add(-24 * time.Hour),
	}

	if session.isExpired() {
		t.Fatalf("expected pinned session to stay active even with an old expiry")
	}
}

func resolveStoreCipherForTest() (*storeCipher, error) {
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	return resolveStoreCipherFromValue(key)
}
