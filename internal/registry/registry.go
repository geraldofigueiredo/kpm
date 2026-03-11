package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Entry is a single known port-forward service stored on disk.
type Entry struct {
	ServiceName     string    `json:"service_name"`
	Namespace       string    `json:"namespace"`
	Port            int       `json:"port"`
	ClusterName     string    `json:"cluster_name"`
	ClusterEnvName  string    `json:"cluster_env_name"`
	ClusterEndpoint string    `json:"cluster_endpoint"`
	ClusterCAData   []byte    `json:"cluster_ca_data"`
	AddedAt         time.Time `json:"added_at"`
}

// Key uniquely identifies an entry.
type Key struct{ Endpoint, Namespace, ServiceName string }

func (e Entry) Key() Key {
	return Key{e.ClusterEndpoint, e.Namespace, e.ServiceName}
}

func registryPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".kpm", "registry.json"), nil
}

func Load() ([]Entry, error) {
	path, err := registryPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func Save(entries []Entry) error {
	path, err := registryPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Add inserts an entry if it doesn't already exist (dedup by endpoint+namespace+service).
func Add(e Entry) error {
	entries, err := Load()
	if err != nil {
		entries = []Entry{}
	}
	for _, existing := range entries {
		if existing.Key() == e.Key() {
			return nil // already known
		}
	}
	if e.AddedAt.IsZero() {
		e.AddedAt = time.Now()
	}
	return Save(append(entries, e))
}

// Remove deletes the entry matching the given key.
func Remove(endpoint, namespace, serviceName string) error {
	entries, err := Load()
	if err != nil {
		return err
	}
	target := Key{endpoint, namespace, serviceName}
	filtered := entries[:0]
	for _, e := range entries {
		if e.Key() != target {
			filtered = append(filtered, e)
		}
	}
	return Save(filtered)
}

// Clear removes all entries from the registry.
func Clear() error {
	return Save([]Entry{})
}
