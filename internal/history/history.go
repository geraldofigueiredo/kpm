package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const maxSessions = 10

// ServiceEntry records a single forwarded service within a session.
type ServiceEntry struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Port      int    `json:"port"`
}

// Session records a complete port-forward session with enough info to
// rebuild all tunnels without calling the GCP API again.
type Session struct {
	Timestamp       time.Time      `json:"timestamp"`
	Project         string         `json:"project"`
	ClusterName     string         `json:"cluster_name"`
	ClusterEnvName  string         `json:"cluster_env_name"`
	ClusterEndpoint string         `json:"cluster_endpoint"`
	ClusterCAData   []byte         `json:"cluster_ca_data"`
	Services        []ServiceEntry `json:"services"`
}

func historyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".kpm", "history.json"), nil
}

func Load() ([]Session, error) {
	path, err := historyPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []Session{}, nil
	}
	if err != nil {
		return nil, err
	}
	var sessions []Session
	if err := json.Unmarshal(data, &sessions); err != nil {
		return nil, err
	}
	return sessions, nil
}

func Save(sessions []Session) error {
	path, err := historyPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func Append(s Session) error {
	sessions, err := Load()
	if err != nil {
		sessions = []Session{}
	}
	// Replace existing entry for the same cluster+services fingerprint if present,
	// otherwise prepend.
	filtered := sessions[:0]
	for _, existing := range sessions {
		if existing.ClusterName != s.ClusterName {
			filtered = append(filtered, existing)
		}
	}
	sessions = append([]Session{s}, filtered...)
	if len(sessions) > maxSessions {
		sessions = sessions[:maxSessions]
	}
	return Save(sessions)
}

// AllServices returns deduplicated (clusterEndpoint, clusterCAData, namespace, service, port)
// entries across all sessions, newest-first. Used to pre-populate the forwards panel.
func AllServices(sessions []Session) []ServiceEntry {
	type key struct{ cluster, ns, name string }
	seen := map[key]bool{}
	var out []ServiceEntry
	for _, s := range sessions {
		for _, svc := range s.Services {
			k := key{s.ClusterEndpoint, svc.Namespace, svc.Name}
			if !seen[k] {
				seen[k] = true
				// Attach cluster info into the entry for resume.
				out = append(out, ServiceEntry{
					Name:      svc.Name,
					Namespace: svc.Namespace,
					Port:      svc.Port,
				})
			}
		}
	}
	return out
}
