package manifest

import (
	"encoding/json"
	"errors"
	"testing"
)

func validManifest() Manifest {
	return Manifest{
		SchemaVersion: SchemaVersion,
		SpecVersion:   "rungrad-spec/1",
		ToolName:      "rgdemo",
		ToolVersion:   "v0.0.0",
		GlobalFlags:   []Flag{},
		Commands: []Command{
			{
				Path:        []string{},
				Examples:    []string{},
				Related:     []string{},
				OutputModes: []string{},
				LocalFlags:  []Flag{},
			},
			{
				Path:        []string{"item", "list"},
				Examples:    []string{},
				Related:     []string{},
				OutputModes: []string{},
				LocalFlags:  []Flag{},
			},
		},
	}
}

func TestValidateValidManifest(t *testing.T) {
	m := validManifest()
	if err := Validate(&m); err != nil {
		t.Fatalf("Validate(valid) = %v", err)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var roundTrip Manifest
	if err := json.Unmarshal(b, &roundTrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := Validate(&roundTrip); err != nil {
		t.Fatalf("Validate(round trip) = %v", err)
	}
}

func TestValidateNilManifest(t *testing.T) {
	if err := Validate(nil); err == nil {
		t.Fatal("Validate(nil) = nil")
	}
}

func TestValidateMissingSchemaVersionIsInvalid(t *testing.T) {
	m := validManifest()
	m.SchemaVersion = ""
	err := Validate(&m)
	if err == nil {
		t.Fatal("Validate missing schema_version = nil")
	}
	var unsupported *UnsupportedVersionError
	if errors.As(err, &unsupported) {
		t.Fatalf("missing schema_version classified as unsupported: %v", err)
	}
}

func TestValidateUnsupportedSchemaVersion(t *testing.T) {
	m := validManifest()
	m.SchemaVersion = "rungrad-manifest/2"
	err := Validate(&m)
	var unsupported *UnsupportedVersionError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Validate unsupported version = %v, want UnsupportedVersionError", err)
	}
	if unsupported.Version != "rungrad-manifest/2" {
		t.Fatalf("unsupported version = %q", unsupported.Version)
	}
}

func TestValidateUnsupportedVersionWinsOverStructure(t *testing.T) {
	m := validManifest()
	m.SchemaVersion = "rungrad-manifest/2"
	m.Commands = nil
	err := Validate(&m)
	var unsupported *UnsupportedVersionError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Validate unsupported malformed = %v, want UnsupportedVersionError", err)
	}
}

func TestValidateRejectsMalformedManifest(t *testing.T) {
	tests := map[string]func(*Manifest){
		"missing tool name": func(m *Manifest) {
			m.ToolName = ""
		},
		"nil commands": func(m *Manifest) {
			m.Commands = nil
		},
		"empty commands": func(m *Manifest) {
			m.Commands = []Command{}
		},
		"missing root": func(m *Manifest) {
			m.Commands = m.Commands[1:]
		},
		"duplicate paths": func(m *Manifest) {
			m.Commands = append(m.Commands, m.Commands[1])
		},
		"nil path": func(m *Manifest) {
			m.Commands[1].Path = nil
		},
		"empty path segment": func(m *Manifest) {
			m.Commands[1].Path = []string{"item", ""}
		},
		"whitespace path segment": func(m *Manifest) {
			m.Commands[1].Path = []string{"item list"}
		},
		"nil global flags": func(m *Manifest) {
			m.GlobalFlags = nil
		},
		"nil examples": func(m *Manifest) {
			m.Commands[1].Examples = nil
		},
		"nil related": func(m *Manifest) {
			m.Commands[1].Related = nil
		},
		"nil output modes": func(m *Manifest) {
			m.Commands[1].OutputModes = nil
		},
		"nil local flags": func(m *Manifest) {
			m.Commands[1].LocalFlags = nil
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			m := validManifest()
			mutate(&m)
			if err := Validate(&m); err == nil {
				t.Fatal("Validate malformed manifest = nil")
			}
		})
	}
}
