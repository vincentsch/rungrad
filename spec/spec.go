// Package spec embeds the written rungrad specification and its machine-readable
// ruleset so the conformance scorer and other tools can read them without
// depending on files being present on disk at runtime.
package spec

import _ "embed"

// Version is the spec version string. It must match the version field in
// ruleset.yaml.
const Version = "rungrad-spec/1"

// RulesetYAML is the embedded machine-readable ruleset.
//
//go:embed ruleset.yaml
var RulesetYAML []byte

// Sections are the spec's section slugs, each backed by a markdown criterion
// document in this directory.
var Sections = []string{
	"output-contract",
	"exit-codes",
	"dry-run",
	"determinism",
	"name-resolution",
	"self-describing-help",
	"self-update",
	"auth-and-config",
}
