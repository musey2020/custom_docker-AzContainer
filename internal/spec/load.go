// Package spec — config.json load/save.
package spec

import (
	"encoding/json"
	"fmt"
	"os"
)

// Load — config.json faylından spec oxuyur.
func Load(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("oxu: %w", err)
	}

	s := &Spec{}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	if err := validate(s); err != nil {
		return nil, fmt.Errorf("yanlış spec: %w", err)
	}

	return s, nil
}

// Save — spec-i config.json kimi yazır.
func Save(s *Spec, path string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// validate — minimal spec yoxlaması.
func validate(s *Spec) error {
	if s.OCIVersion == "" {
		return fmt.Errorf("ociVersion boşdur")
	}
	if s.Process == nil {
		return fmt.Errorf("process yoxdur")
	}
	if len(s.Process.Args) == 0 {
		return fmt.Errorf("process.args boşdur")
	}
	if s.Root == nil || s.Root.Path == "" {
		return fmt.Errorf("root.path boşdur")
	}
	return nil
}

// HasNamespace — verilmiş tipli namespace spec-də varmı?
func (s *Spec) HasNamespace(nsType string) bool {
	if s.Linux == nil {
		return false
	}
	for _, ns := range s.Linux.Namespaces {
		if ns.Type == nsType {
			return true
		}
	}
	return false
}
