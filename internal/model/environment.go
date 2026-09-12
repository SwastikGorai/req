package model

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Environment struct {
	SchemaVersion int            `json:"schema_version"`
	Name          string         `json:"name"`
	Variables     map[string]any `json:"variables"`
}

func ValidEnvironmentName(name string) bool {
	if !ValidID(name) {
		return false
	}
	u := strings.ToUpper(name)
	if u == "CON" || u == "PRN" || u == "AUX" || u == "NUL" {
		return false
	}
	return !(len(u) == 4 && (strings.HasPrefix(u, "COM") || strings.HasPrefix(u, "LPT")) && u[3] >= '1' && u[3] <= '9')
}

func ValidateVariables(values map[string]any) error {
	for key := range values {
		if key == "" || strings.HasPrefix(key, "env:") {
			return fmt.Errorf("invalid variable name %q (env: is reserved)", key)
		}
	}
	_, err := json.Marshal(values)
	return err
}

func (e Environment) Validate() error {
	if e.SchemaVersion != SchemaVersion {
		return &SchemaVersionError{Found: e.SchemaVersion, Want: SchemaVersion}
	}
	if !ValidEnvironmentName(e.Name) {
		return fmt.Errorf("invalid environment name %q", e.Name)
	}
	return ValidateVariables(e.Variables)
}
