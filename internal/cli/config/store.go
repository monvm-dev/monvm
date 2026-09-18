// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Deployment is the local metadata needed to manage an existing deployment.
type Deployment struct {
	Name      string          `json:"name"`
	Region    string          `json:"region"`
	Profile   string          `json:"profile,omitempty"`
	Bucket    string          `json:"bucket"`
	Variables json.RawMessage `json:"variables"`
}

// Store persists deployment metadata in a local directory.
type Store struct{ Directory string }

var ErrNotFound = errors.New("deployment is not configured locally")

// ConfigDir returns the platform-appropriate MonVM configuration directory.
func ConfigDir() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "monvm"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "linux" {
		return filepath.Join(home, ".config", "monvm"), nil
	}
	return filepath.Join(home, ".monvm", "config"), nil
}

// CacheDir returns the platform-appropriate MonVM cache directory.
func CacheDir() (string, error) {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "monvm"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "linux" {
		return filepath.Join(home, ".cache", "monvm"), nil
	}
	return filepath.Join(home, ".monvm", "cache"), nil
}

// DefaultStore returns the deployment store under the default configuration directory.
func DefaultStore() (*Store, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	return &Store{Directory: filepath.Join(dir, "deployments")}, nil
}

// Load reads and decodes a deployment by name.
func (s *Store) Load(name string) (Deployment, error) {
	body, err := os.ReadFile(filepath.Join(s.Directory, name+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return Deployment{}, fmt.Errorf("%w: %q; run monvm setup %s", ErrNotFound, name, name)
	}
	if err != nil {
		return Deployment{}, err
	}
	var deployment Deployment
	if err = json.Unmarshal(body, &deployment); err != nil {
		return Deployment{}, fmt.Errorf("read deployment %q: %w", name, err)
	}
	return deployment, nil
}

// Save atomically persists deployment metadata and synchronizes its directory entry.
func (s *Store) Save(deployment Deployment) error {
	if err := os.MkdirAll(s.Directory, 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(deployment, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	file, err := os.CreateTemp(s.Directory, ".deployment-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer func() { _ = os.Remove(temporary) }()
	if err = file.Chmod(0o600); err == nil {
		_, err = file.Write(body)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(temporary, filepath.Join(s.Directory, deployment.Name+".json")); err != nil {
		return err
	}
	directory, err := os.Open(s.Directory)
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	return directory.Sync()
}

// Delete removes local metadata for a deployment if it exists.
func (s *Store) Delete(name string) error {
	err := os.Remove(filepath.Join(s.Directory, name+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
