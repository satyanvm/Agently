package platform

// secretbox.go — at-rest encryption for credentials.data.
//
// Values are sealed with AES-256-GCM under AGENTLY_CREDENTIALS_KEY (32 bytes,
// base64 or hex) and stored in the same jsonb column as
//
//	{"$enc": "v1:<base64(nonce || ciphertext)>"}
//
// The reasoner (reasoner/db.py) and pieces worker (src/credentials.ts) open the
// same envelope. Rows without "$enc" are legacy plaintext: they still read, and
// EncryptPlaintextCredentials seals them at API startup.

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	encField  = "$enc"
	encPrefix = "v1:"
)

var (
	cipherOnce sync.Once
	cipherAEAD cipher.AEAD
	cipherErr  error
)

// CredentialsKeyConfigured reports whether AGENTLY_CREDENTIALS_KEY is set,
// and fails when it is set but malformed.
func CredentialsKeyConfigured() (bool, error) {
	aead, err := credentialCipher()
	return aead != nil, err
}

func credentialCipher() (cipher.AEAD, error) {
	cipherOnce.Do(func() {
		cipherAEAD, cipherErr = loadCipher(os.Getenv("AGENTLY_CREDENTIALS_KEY"))
	})
	return cipherAEAD, cipherErr
}

func loadCipher(raw string) (cipher.AEAD, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	key, err := decodeKey(raw)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func decodeKey(raw string) ([]byte, error) {
	if b, err := hex.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil && len(b) == 32 {
		return b, nil
	}
	return nil, errors.New("AGENTLY_CREDENTIALS_KEY must be 32 bytes, hex or base64 encoded (openssl rand -hex 32)")
}

// sealCredentialData returns the jsonb value to store for data: the sealed
// envelope when a key is configured, else plaintext.
func sealCredentialData(data map[string]any) (any, error) {
	aead, err := credentialCipher()
	if err != nil {
		return nil, err
	}
	plain, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	if aead == nil {
		return string(plain), nil
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	sealed := aead.Seal(nonce, nonce, plain, nil)
	return jsonArg(map[string]string{encField: encPrefix + base64.StdEncoding.EncodeToString(sealed)}), nil
}

// openCredentialData decodes a stored data column, decrypting the envelope.
func openCredentialData(raw []byte) (map[string]any, error) {
	out := map[string]any{}
	var env map[string]any
	jsonInto(raw, &env)
	enc, ok := env[encField].(string)
	if !ok {
		jsonInto(raw, &out)
		return out, nil
	}
	aead, err := credentialCipher()
	if err != nil {
		return nil, err
	}
	if aead == nil {
		return nil, errors.New("credential is encrypted but AGENTLY_CREDENTIALS_KEY is not set")
	}
	sealed, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(enc, encPrefix))
	if err != nil || !strings.HasPrefix(enc, encPrefix) || len(sealed) < aead.NonceSize() {
		return nil, errors.New("credential envelope is malformed")
	}
	n := aead.NonceSize()
	plain, err := aead.Open(nil, sealed[:n], sealed[n:], nil)
	if err != nil {
		return nil, errors.New("credential could not be decrypted — wrong AGENTLY_CREDENTIALS_KEY?")
	}
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// EncryptPlaintextCredentials seals every legacy plaintext row. A no-op when
// no key is configured. Returns how many rows were sealed.
func EncryptPlaintextCredentials(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	if aead, err := credentialCipher(); aead == nil || err != nil {
		return 0, err
	}
	rows, err := pool.Query(ctx, `select id, data from credentials where not (data ? '`+encField+`')`)
	if err != nil {
		return 0, err
	}
	type row struct {
		id   string
		data map[string]any
	}
	var todo []row
	for rows.Next() {
		var r row
		var raw []byte
		if err := rows.Scan(&r.id, &raw); err != nil {
			rows.Close()
			return 0, err
		}
		r.data = map[string]any{}
		jsonInto(raw, &r.data)
		todo = append(todo, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, r := range todo {
		sealed, err := sealCredentialData(r.data)
		if err != nil {
			return 0, err
		}
		if _, err := pool.Exec(ctx, `update credentials set data=$1 where id=$2`, sealed, r.id); err != nil {
			return 0, fmt.Errorf("seal credential %s: %w", r.id, err)
		}
	}
	return len(todo), nil
}
