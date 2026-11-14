package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const prefix = "enc:v1:"

var (
	keyMu            sync.RWMutex
	credentialCipher cipher.AEAD
)

// Init loads a deployment key or creates a private, persistent development key.
// Losing this key makes stored mailbox credentials unreadable.
func Init(encodedKey, keyPath string) error {
	if encodedKey == "" {
		data, err := os.ReadFile(keyPath)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
				return fmt.Errorf("create credential key directory: %w", err)
			}
			key := make([]byte, 32)
			if _, err := rand.Read(key); err != nil {
				return fmt.Errorf("generate credential key: %w", err)
			}
			encoded := base64.StdEncoding.EncodeToString(key)
			file, createErr := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if errors.Is(createErr, os.ErrExist) {
				data, err = os.ReadFile(keyPath)
			} else if createErr != nil {
				return fmt.Errorf("create credential key: %w", createErr)
			} else {
				_, writeErr := file.WriteString(encoded + "\n")
				syncErr := file.Sync()
				closeErr := file.Close()
				if writeErr != nil || syncErr != nil || closeErr != nil {
					return errors.New("failed to persist credential key")
				}
				data, err = []byte(encoded), nil
			}
		}
		if err != nil {
			return fmt.Errorf("read credential key: %w", err)
		}
		info, err := os.Stat(keyPath)
		if err != nil || info.Mode().Perm()&0077 != 0 {
			return errors.New("credential key file must be accessible only by its owner (0600)")
		}
		encodedKey = strings.TrimSpace(string(data))
	}
	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil || len(key) != 32 {
		return errors.New("CREDENTIALS_KEY must be a base64-encoded 32-byte key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	keyMu.Lock()
	credentialCipher = aead
	keyMu.Unlock()
	return nil
}

func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, prefix)
}

func Encrypt(plaintext string) (string, error) {
	keyMu.RLock()
	aead := credentialCipher
	keyMu.RUnlock()
	if aead == nil {
		return "", errors.New("credential encryption is not initialized")
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", errors.New("generate credential nonce failed")
	}
	sealed := aead.Seal(nonce, nonce, []byte(plaintext), []byte(prefix))
	return prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt also accepts transient plaintext used to test an unsaved mailbox.
// Persisted accounts are encrypted and authenticated during startup migration.
func Decrypt(value string) (string, error) {
	if !IsEncrypted(value) {
		return value, nil
	}
	keyMu.RLock()
	aead := credentialCipher
	keyMu.RUnlock()
	if aead == nil {
		return "", errors.New("credential encryption is not initialized")
	}
	sealed, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	if err != nil || len(sealed) < aead.NonceSize()+aead.Overhead() {
		return "", errors.New("invalid encrypted credential")
	}
	plain, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], []byte(prefix))
	if err != nil {
		return "", errors.New("credential authentication failed; check CREDENTIALS_KEY")
	}
	return string(plain), nil
}
