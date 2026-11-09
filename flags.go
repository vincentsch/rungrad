package rungrad

import "github.com/spf13/pflag"

// GlobalFlags are the persistent flags every rungrad tool exposes. They are
// registered once on the root command and read by commands through the Factory,
// so a tool never wires them by hand.
type GlobalFlags struct {
	JSON        bool   // emit stable JSON instead of the human view
	DryRun      bool   // preview mutations without performing them
	NoPrompt    bool   // never block on an interactive prompt
	Quiet       bool   // suppress non-essential human prose
	Config      string // path to the config file
	Profile     string // active profile for resolution-enabled apps
	AuthFile    string // credentials file path for resolution-enabled apps
	Plain       bool   // emit explicit unstyled, copy-safe text
	JQ          string // transform stable JSON with a jq expression
	Template    string // render stable JSON through a Go text/template
	IncludeMeta bool   // wrap machine output as {data, meta}
	NoColor     bool   // disable color while allowing non-color ANSI
	NoANSI      bool   // disable all terminal control bytes; implies no color and no pager
	NoPager     bool   // disable pager use for human output
}

// register attaches the global flags to a persistent flag set.
func (g *GlobalFlags) register(fs *pflag.FlagSet) {
	fs.BoolVar(&g.JSON, "json", false, "Output stable JSON instead of the human view")
	fs.BoolVar(&g.DryRun, "dry-run", false, "Preview changes without performing them")
	fs.BoolVar(&g.NoPrompt, "no-prompt", false, "Never block on an interactive prompt")
	fs.BoolVar(&g.Quiet, "quiet", false, "Suppress non-essential output")
	fs.StringVar(&g.Config, "config", "", "Path to the config file")
}

// registerAdvanced attaches opt-in output flags. Keeping these separate means
// existing apps do not accept modes they have not audited command by command.
func (g *GlobalFlags) registerAdvanced(fs *pflag.FlagSet) {
	fs.BoolVar(&g.Plain, "plain", false, usagePlain)
	fs.StringVar(&g.JQ, "jq", "", usageJQ)
	fs.StringVar(&g.Template, "template", "", usageTemplate)
	fs.BoolVar(&g.IncludeMeta, "include-meta", false, usageIncludeMeta)
	fs.BoolVar(&g.NoColor, "no-color", false, "Disable color in human output")
	fs.BoolVar(&g.NoANSI, "no-ansi", false, "Disable all ANSI/control sequences in human output")
	fs.BoolVar(&g.NoPager, "no-pager", false, "Never use a pager for long human output")
	for id, capability := range flagCapabilities {
		markFlagCapability(fs.Lookup(id), capability)
	}
}

// Usage strings of the output flags that only some commands accept. Help shows
// each of them only on commands that accept it, so the text needs no caveat.
const (
	usagePlain       = "Print text without color or formatting"
	usageJQ          = "Filter the JSON output with a jq expression"
	usageTemplate    = "Format the JSON output with a Go text/template"
	usageIncludeMeta = "Wrap the JSON output as {data, meta} with request details"
)

// flagCapabilities maps the rungrad identity of each output flag that only
// some commands accept to the command capability it needs.
var flagCapabilities = map[string]string{
	"plain":        capabilityPlain,
	"jq":           capabilityTransform,
	"template":     capabilityTransform,
	"include-meta": capabilityMeta,
}

const (
	capabilityPlain     = "plain"
	capabilityTransform = "transform"
	capabilityMeta      = "meta"

	// flagCapabilityAnnotation names the command capability a global flag
	// needs. Help leaves the flag out on commands without it.
	flagCapabilityAnnotation = "rungrad.requires"
)

// markFlagCapability records on f which command capability it needs.
func markFlagCapability(f *pflag.Flag, capability string) {
	if f == nil {
		return
	}
	if f.Annotations == nil {
		f.Annotations = map[string][]string{}
	}
	f.Annotations[flagCapabilityAnnotation] = []string{capability}
}

// registerResolution attaches opt-in config/auth resolution flags.
func (g *GlobalFlags) registerResolution(fs *pflag.FlagSet, rc *ResolutionConfig) {
	if rc == nil {
		return
	}
	if rc.Profile {
		fs.StringVar(&g.Profile, "profile", "", "Profile to use for config and credentials")
	}
	if rc.AuthFile {
		fs.StringVar(&g.AuthFile, "auth-file", "", "Path to the credentials file")
	}
}
