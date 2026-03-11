package profiles

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/geraldofigueiredo/kportmaster/internal/registry"
)

// Profile is a named set of port-forward services that can be activated together.
type Profile struct {
	Name      string           `json:"name"`
	Services  []registry.Entry `json:"services"`
	CreatedAt time.Time        `json:"created_at"`
}

func profilesPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".kpm", "profiles.json"), nil
}

func Load() ([]Profile, error) {
	path, err := profilesPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []Profile{}, nil
	}
	if err != nil {
		return nil, err
	}
	var profs []Profile
	if err := json.Unmarshal(data, &profs); err != nil {
		return nil, err
	}
	return profs, nil
}

func Save(profs []Profile) error {
	path, err := profilesPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(profs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Upsert saves the profile, overwriting any existing profile with the same name.
func Upsert(p Profile) error {
	profs, err := Load()
	if err != nil {
		profs = []Profile{}
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}
	for i, existing := range profs {
		if existing.Name == p.Name {
			profs[i] = p
			return Save(profs)
		}
	}
	return Save(append(profs, p))
}

// Remove deletes the profile with the given name.
func Remove(name string) error {
	profs, err := Load()
	if err != nil {
		return err
	}
	filtered := profs[:0]
	for _, p := range profs {
		if p.Name != name {
			filtered = append(filtered, p)
		}
	}
	return Save(filtered)
}
