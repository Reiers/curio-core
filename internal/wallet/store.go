package wallet

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type KeyType string

const (
	KeyTypeSecp      KeyType = "secp"
	KeyTypeBLS       KeyType = "bls"
	KeyTypeDelegated KeyType = "delegated"
)

type Entry struct {
	Name       string    `json:"name"`
	Address    string    `json:"address"`
	KeyType    KeyType   `json:"keyType"`
	PrivateKey string    `json:"privateKey"`
	CreatedAt  time.Time `json:"createdAt"`
}

type Store struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

func NewStore() *Store { return &Store{Version: 1, Entries: []Entry{}} }

func walletFile(home string) string {
	return filepath.Join(home, "wallet.json.enc")
}

func Load(home, passphrase string) (*Store, error) {
	path := walletFile(home)
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewStore(), nil
		}
		return nil, err
	}
	plain, err := decrypt(b, passphrase)
	if err != nil {
		return nil, fmt.Errorf("wallet decrypt failed (wrong password or corrupted file): %w", err)
	}
	st := NewStore()
	if err := json.Unmarshal(plain, st); err != nil {
		return nil, err
	}
	return st, nil
}

func Save(home, passphrase string, st *Store) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	plain, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	enc, err := encrypt(plain, passphrase)
	if err != nil {
		return err
	}
	return os.WriteFile(walletFile(home), enc, 0o600)
}

func (s *Store) Add(name string, kt KeyType) (Entry, error) {
	if kt != KeyTypeSecp && kt != KeyTypeBLS && kt != KeyTypeDelegated {
		return Entry{}, fmt.Errorf("unsupported key type %q (supported: secp|bls|delegated)", kt)
	}
	for _, e := range s.Entries {
		if strings.EqualFold(e.Name, name) {
			return Entry{}, fmt.Errorf("wallet %q already exists", name)
		}
	}
	randPart := randomHex(20)
	addrPrefix := "f1"
	switch kt {
	case KeyTypeBLS:
		addrPrefix = "f3"
	case KeyTypeDelegated:
		addrPrefix = "f4"
	}
	e := Entry{
		Name:       name,
		Address:    addrPrefix + randomHex(18),
		KeyType:    kt,
		PrivateKey: "alpha_" + randPart,
		CreatedAt:  time.Now().UTC(),
	}
	s.Entries = append(s.Entries, e)
	return e, nil
}

func (s *Store) FindByNameOrAddress(q string) (*Entry, error) {
	for i := range s.Entries {
		e := &s.Entries[i]
		if strings.EqualFold(e.Name, q) || e.Address == q {
			return e, nil
		}
	}
	return nil, fmt.Errorf("wallet %q not found", q)
}

func encrypt(plaintext []byte, passphrase string) ([]byte, error) {
	key := sha256.Sum256([]byte("curio-alpha-salt::" + passphrase))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	out := gcm.Seal(nil, nonce, plaintext, nil)
	return append(nonce, out...), nil
}

func decrypt(ciphertext []byte, passphrase string) ([]byte, error) {
	key := sha256.Sum256([]byte("curio-alpha-salt::" + passphrase))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	nonce, payload := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, payload, nil)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "deadbeef"
	}
	return hex.EncodeToString(b)
}
