package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/janiorvalle/better-git-review/internal/document"
	"github.com/janiorvalle/better-git-review/internal/xdg"
)

type Validator func(document.Document) error

type Cache struct {
	Dir      string
	Validate Validator
}

func Key(diff []byte, providerName, model, reasoning string, schemaVersion int) string {
	hash := sha256.New()
	writePart := func(part []byte) {
		fmt.Fprintf(hash, "%d:", len(part))
		hash.Write(part)
	}
	writePart(diff)
	writePart([]byte(providerName))
	writePart([]byte(model))
	writePart([]byte(reasoning))
	writePart([]byte(fmt.Sprintf("%d", schemaVersion)))
	return hex.EncodeToString(hash.Sum(nil))
}

func Default(validate Validator) (Cache, error) {
	cacheDir, err := xdg.CacheDir()
	if err != nil {
		return Cache{}, err
	}
	return Cache{Dir: cacheDir, Validate: validate}, nil
}

func (c Cache) Load(key string) (document.Document, bool) {
	data, err := os.ReadFile(filepath.Join(c.Dir, key+".json"))
	if err != nil {
		return document.Document{}, false
	}
	var result document.Document
	if err := json.Unmarshal(data, &result); err != nil {
		return document.Document{}, false
	}
	var required struct {
		Meta struct {
			Staged *bool `json:"staged"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(data, &required); err != nil || required.Meta.Staged == nil {
		return document.Document{}, false
	}
	if result.SchemaVersion != document.SchemaVersion {
		return document.Document{}, false
	}
	if c.Validate != nil && c.Validate(result) != nil {
		return document.Document{}, false
	}
	result.Meta.Cached = true
	return result, true
}

func (c Cache) Store(key string, value document.Document) error {
	if err := os.MkdirAll(c.Dir, 0o700); err != nil {
		return err
	}
	value.Meta.Cached = false
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(c.Dir, ".cache-*.json")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(append(encoded, '\n')); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, filepath.Join(c.Dir, key+".json")); err != nil {
		return err
	}
	return nil
}
