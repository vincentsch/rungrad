// Package scaffold generates a new rungrad CLI project: a go.mod, a complete
// main.go built on the framework with a widget resource (list/create/delete)
// demonstrating dual output, a mutating dry-run, a name-direct destructive
// command, and an offline update command, a passing in-process test suite, and a
// README. The output builds, runs, and scores well against the spec immediately.
package scaffold

import (
	"bytes"
	"embed"
	"fmt"
	"go/format"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"github.com/vincentsch/rungrad/manifest"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

// compactTemplateMap maps embedded templates to output paths for the compact
// starter profile.
var compactTemplateMap = map[string]string{
	"templates/gomod.tmpl":        "go.mod",
	"templates/main.go.tmpl":      "main.go",
	"templates/main_test.go.tmpl": "main_test.go",
	"templates/readme.md.tmpl":    "README.md",
}

// productTemplateMap maps embedded templates to output paths for the product
// profile.
var productTemplateMap = map[string]string{
	"templates/gomod.tmpl":                "go.mod",
	"templates/product_main.go.tmpl":      "main.go",
	"templates/product_main_test.go.tmpl": "main_test.go",
	"templates/product_readme.md.tmpl":    "README.md",
}

// Options configures a scaffold.
type Options struct {
	// Name is the program name and binary name.
	Name string
	// Module is the Go module path. Defaults to "example.com/<name>".
	Module string
	// RungradReplace, when set, adds a replace directive pointing the rungrad
	// dependency at a local path. Used by tests to build without a published
	// module; leave empty for a real project.
	RungradReplace string

	// ProductProfile enables the expanded product CLI scaffold. When false, all
	// product fields must be left at their zero value.
	ProductProfile bool
	// EnvPrefix is the product environment-variable prefix. Defaults to a value
	// derived from Name.
	EnvPrefix string
	// ProductName is AppConfig.Short. Defaults to "<name> CLI".
	ProductName string
	// Description is AppConfig.Long.
	Description string
	// DocsLabel is the README/docs title. Defaults to ProductName.
	DocsLabel string
	// Services are name=url service endpoint specs. Repeating the default "api"
	// service replaces its URL; other names append services in order.
	Services []string
	// MetadataNamespace is the manifest extension namespace.
	MetadataNamespace string
	// Surface selects global-flag ownership: "rungrad" or "host".
	Surface string
	// ReleaseOwner and ReleaseRepo are safe placeholders rendered only in
	// comments/docs.
	ReleaseOwner string
	ReleaseRepo  string
	// Examples are extra full command invocations appended to root and matching
	// leaf-command examples.
	Examples []string
}

// ValidationError reports invalid scaffold input. The root package maps it to a
// usage exit code through the ExitCode method.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

func (e *ValidationError) ExitCode() int { return 1 }

var validName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
var validProductEnvPrefixRE = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
var validReleaseSlugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func (o Options) module() string {
	if o.Module != "" {
		return o.Module
	}
	return "example.com/" + o.Name
}

func envVarName(name string) string {
	return strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_TOKEN"
}

func validateNameModule(o Options) error {
	if o.Name == "" {
		return &ValidationError{Message: "scaffold: name is required"}
	}
	if !validName.MatchString(o.Name) {
		return &ValidationError{Message: "scaffold: name must start with a lowercase letter and contain only lowercase letters, digits, and hyphens"}
	}
	module := o.module()
	if module == "" || strings.ContainsAny(module, " \t\r\n\"'\\") || strings.Contains(module, "..") || strings.HasPrefix(module, "/") || strings.HasSuffix(module, "/") {
		return &ValidationError{Message: "scaffold: module path is invalid"}
	}
	return nil
}

// Generate returns the project files as a map of relative path to content.
func Generate(o Options) (map[string]string, error) {
	if err := validateNameModule(o); err != nil {
		return nil, err
	}
	if !o.ProductProfile {
		if err := rejectProductFields(o); err != nil {
			return nil, err
		}
		return render(compactTemplateMap, compactData(o))
	}
	data, err := productData(o)
	if err != nil {
		return nil, err
	}
	return render(productTemplateMap, data)
}

func render(templateMap map[string]string, data any) (map[string]string, error) {
	files := map[string]string{}
	for src, dst := range templateMap {
		raw, err := templateFS.ReadFile(src)
		if err != nil {
			return nil, err
		}
		t, err := template.New(dst).Funcs(template.FuncMap{
			"goquote": strconv.Quote,
			"joinLines": func(xs []string) string {
				return strings.Join(xs, "\n")
			},
		}).Parse(string(raw))
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		if err := t.Execute(&buf, data); err != nil {
			return nil, err
		}
		content := buf.String()
		if filepath.Ext(dst) == ".go" {
			formatted, err := format.Source([]byte(content))
			if err != nil {
				return nil, fmt.Errorf("format %s: %w", dst, err)
			}
			content = string(formatted)
		}
		files[dst] = content
	}
	return files, nil
}

type compactTemplateData struct {
	Name           string
	Module         string
	EnvVar         string
	RungradReplace string
}

func compactData(o Options) compactTemplateData {
	return compactTemplateData{
		Name:           o.Name,
		Module:         o.module(),
		EnvVar:         envVarName(o.Name),
		RungradReplace: o.RungradReplace,
	}
}

// productTemplateData is fully derived before rendering product templates. That
// keeps the templates mostly declarative and keeps validation errors in Go code
// instead of surfacing later as generated app panics.
type productTemplateData struct {
	Name           string
	Module         string
	RungradReplace string

	EnvPrefix      string
	EnvVar         string
	ProfileEnvVar  string
	AuthFileEnvVar string
	ConfigEnvVar   string

	ProductName string
	Description string
	DocsLabel   string

	Services              []serviceData
	PrimaryService        string
	PrimaryServiceFlag    string
	PrimaryServiceDefault string

	MetadataNamespace string
	HostSurface       bool
	ReleaseOwner      string
	ReleaseRepo       string

	RootExamples         []string
	WidgetListExamples   []string
	WidgetCreateExamples []string
	WidgetDeleteExamples []string
	UpdateExamples       []string
}

// serviceData is the generated shape of one rungrad service endpoint: the
// public flag, env var, config key, default URL, and help text all derive from
// the stable service name.
type serviceData struct {
	Name      string
	Flag      string
	EnvVar    string
	ConfigKey string
	Default   string
	Usage     string
}

// rejectProductFields protects compact-scaffold compatibility. Options cannot
// tell whether a CLI flag was explicitly supplied with an empty value, so
// cmd/rungrad also checks Cobra's Changed bit before building Options.
func rejectProductFields(o Options) error {
	type field struct {
		flag string
		set  bool
	}
	for _, f := range []field{
		{flag: "--env-prefix", set: o.EnvPrefix != ""},
		{flag: "--product-name", set: o.ProductName != ""},
		{flag: "--description", set: o.Description != ""},
		{flag: "--docs-label", set: o.DocsLabel != ""},
		{flag: "--service", set: len(o.Services) > 0},
		{flag: "--metadata-namespace", set: o.MetadataNamespace != ""},
		{flag: "--surface", set: o.Surface != ""},
		{flag: "--release-owner", set: o.ReleaseOwner != ""},
		{flag: "--release-repo", set: o.ReleaseRepo != ""},
		{flag: "--example", set: len(o.Examples) > 0},
	} {
		if f.set {
			return &ValidationError{Message: fmt.Sprintf("scaffold: %s requires --product-profile", f.flag)}
		}
	}
	return nil
}

// productData validates all product-profile inputs and computes the final
// template values in one place. The generated tree should already be safe to
// build; it should not depend on framework panics to catch scaffold mistakes.
func productData(o Options) (productTemplateData, error) {
	envPrefix := o.EnvPrefix
	if envPrefix == "" {
		envPrefix = deriveEnvPrefix(o.Name)
	}
	if !validProductEnvPrefix(envPrefix) {
		return productTemplateData{}, &ValidationError{Message: "scaffold: env prefix must start with an uppercase letter, contain only uppercase letters, digits, and underscores, and not end with underscore"}
	}

	services, err := productServices(envPrefix, o.Services)
	if err != nil {
		return productTemplateData{}, err
	}

	namespace := o.MetadataNamespace
	if namespace == "" {
		namespace = "example.com/" + o.Name
	}
	extensions := manifest.ExtensionSet{
		namespace: {
			"owner":  "platform",
			"status": "stable",
		},
	}
	if err := manifest.ValidateExtensionSet(extensions); err != nil {
		return productTemplateData{}, &ValidationError{Message: "scaffold: metadata namespace is invalid: " + err.Error()}
	}

	surface := o.Surface
	if surface == "" {
		surface = "rungrad"
	}
	if surface != "rungrad" && surface != "host" {
		return productTemplateData{}, &ValidationError{Message: "scaffold: surface must be rungrad or host"}
	}

	releaseOwner := o.ReleaseOwner
	if releaseOwner == "" {
		releaseOwner = "example"
	}
	if err := validateReleaseSlug("--release-owner", releaseOwner, o.ReleaseOwner != ""); err != nil {
		return productTemplateData{}, err
	}
	releaseRepo := o.ReleaseRepo
	if releaseRepo == "" {
		releaseRepo = o.Name
	}
	if err := validateReleaseSlug("--release-repo", releaseRepo, o.ReleaseRepo != ""); err != nil {
		return productTemplateData{}, err
	}

	productName := o.ProductName
	if productName == "" {
		productName = o.Name + " CLI"
	}
	if containsControl(productName) {
		return productTemplateData{}, &ValidationError{Message: "scaffold: product name must not contain control characters"}
	}
	description := o.Description
	if description == "" {
		description = o.Name + " is a product CLI built on the rungrad framework."
	}
	if containsControl(description) {
		return productTemplateData{}, &ValidationError{Message: "scaffold: description must not contain control characters"}
	}
	docsLabel := o.DocsLabel
	if docsLabel == "" {
		docsLabel = productName
	}
	if containsControl(docsLabel) {
		return productTemplateData{}, &ValidationError{Message: "scaffold: docs label must not contain control characters"}
	}

	rootExamples := []string{
		o.Name + " widget list",
		o.Name + " widget list --json",
		o.Name + " widget create gamma --dry-run",
	}
	widgetListExamples := []string{o.Name + " widget list", o.Name + " widget list --json"}
	widgetCreateExamples := []string{o.Name + " widget create gamma", o.Name + " widget create gamma --dry-run"}
	widgetDeleteExamples := []string{o.Name + " widget delete alpha --dry-run", o.Name + " widget delete alpha --confirm"}
	var updateExamples []string
	valueFlags := productExampleValueFlags(services)
	for _, example := range o.Examples {
		target, err := validateExample(o.Name, valueFlags, example)
		if err != nil {
			return productTemplateData{}, err
		}
		rootExamples = dedupAppend(rootExamples, example)
		switch target {
		case "widget list":
			widgetListExamples = dedupAppend(widgetListExamples, example)
		case "widget create":
			widgetCreateExamples = dedupAppend(widgetCreateExamples, example)
		case "widget delete":
			widgetDeleteExamples = dedupAppend(widgetDeleteExamples, example)
		case "update":
			updateExamples = dedupAppend(updateExamples, example)
		}
	}

	return productTemplateData{
		Name:           o.Name,
		Module:         o.module(),
		RungradReplace: o.RungradReplace,

		EnvPrefix:      envPrefix,
		EnvVar:         envPrefix + "_TOKEN",
		ProfileEnvVar:  envPrefix + "_PROFILE",
		AuthFileEnvVar: envPrefix + "_AUTH_FILE",
		ConfigEnvVar:   envPrefix + "_CONFIG",

		ProductName: productName,
		Description: description,
		DocsLabel:   docsLabel,

		Services:              services,
		PrimaryService:        services[0].Name,
		PrimaryServiceFlag:    services[0].Flag,
		PrimaryServiceDefault: services[0].Default,

		MetadataNamespace: namespace,
		HostSurface:       surface == "host",
		ReleaseOwner:      releaseOwner,
		ReleaseRepo:       releaseRepo,

		RootExamples:         rootExamples,
		WidgetListExamples:   widgetListExamples,
		WidgetCreateExamples: widgetCreateExamples,
		WidgetDeleteExamples: widgetDeleteExamples,
		UpdateExamples:       updateExamples,
	}, nil
}

// productServices starts with the default api service, then merges explicit
// name=url specs. Supplying api replaces the default URL in place so api remains
// the primary service used by generated dry-run previews.
func productServices(envPrefix string, specs []string) ([]serviceData, error) {
	services := []serviceData{serviceFromNameURL(envPrefix, "api", "https://api.example.invalid")}
	explicitNames := map[string]bool{}
	for _, raw := range specs {
		name, endpoint, err := parseServiceSpec(raw)
		if err != nil {
			return nil, err
		}
		if explicitNames[name] {
			return nil, &ValidationError{Message: "scaffold: service names must be unique"}
		}
		explicitNames[name] = true
		if err := validServiceURL(endpoint); err != nil {
			return nil, err
		}
		svc := serviceFromNameURL(envPrefix, name, endpoint)
		replaced := false
		for i := range services {
			if services[i].Name == name {
				services[i] = svc
				replaced = true
				break
			}
		}
		if !replaced {
			services = append(services, svc)
		}
	}
	return services, nil
}

func serviceFromNameURL(envPrefix, name, endpoint string) serviceData {
	envName := strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
	configKey := strings.ReplaceAll(name, "-", "_") + "_url"
	return serviceData{
		Name:      name,
		Flag:      name + "-url",
		EnvVar:    envPrefix + "_" + envName + "_URL",
		ConfigKey: configKey,
		Default:   endpoint,
		Usage:     "Base URL for the " + name + " service",
	}
}

// deriveEnvPrefix mirrors rungrad's internal tool env-var derivation for the
// empty suffix case, keeping generated profile/auth/config/service env vars on
// one consistent prefix.
func deriveEnvPrefix(name string) string {
	var b strings.Builder
	lastUnderscore := false
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
			b.WriteByte(c - ('a' - 'A'))
			lastUnderscore = false
		case (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9'):
			b.WriteByte(c)
			lastUnderscore = false
		default:
			if b.Len() > 0 && !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

func parseServiceSpec(raw string) (string, string, error) {
	name, endpoint, ok := strings.Cut(raw, "=")
	if !ok || name == "" || endpoint == "" {
		return "", "", &ValidationError{Message: "scaffold: service must be name=url"}
	}
	if !validName.MatchString(name) {
		return "", "", &ValidationError{Message: "scaffold: service name must start with a lowercase letter and contain only lowercase letters, digits, and hyphens"}
	}
	if frameworkGlobalFlagName(name + "-url") {
		return "", "", &ValidationError{Message: "scaffold: service flag collides with a rungrad global flag"}
	}
	return name, endpoint, nil
}

// productExampleValueFlags lists generated long flags that consume values.
// validateExampleFlagTail treats all other allowed flags as booleans.
func productExampleValueFlags(services []serviceData) map[string]bool {
	flags := map[string]bool{
		"config":    true,
		"profile":   true,
		"auth-file": true,
	}
	for _, svc := range services {
		flags[svc.Flag] = true
	}
	return flags
}

func validProductEnvPrefix(s string) bool {
	return validProductEnvPrefixRE.MatchString(s) && !strings.HasSuffix(s, "_")
}

func validReleaseSlug(s string) bool {
	return len(s) <= 100 && validReleaseSlugRE.MatchString(s)
}

func validateReleaseSlug(flag, value string, explicit bool) error {
	if !validReleaseSlug(value) {
		return &ValidationError{Message: fmt.Sprintf("scaffold: %s must be a lowercase slug of 100 bytes or fewer", flag)}
	}
	if explicit && looksSecretSlug(value) {
		return &ValidationError{Message: fmt.Sprintf("scaffold: %s looks like a credential, not a release placeholder", flag)}
	}
	return nil
}

func looksSecretSlug(s string) bool {
	for _, prefix := range []string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_", "xox", "sk_", "pat_"} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

func validServiceURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return &ValidationError{Message: "scaffold: service URL is invalid"}
	}
	if u.Scheme != "https" || u.User != nil || u.Host == "" || !strings.HasSuffix(u.Hostname(), ".invalid") {
		return &ValidationError{Message: "scaffold: service URL must be an https URL under .invalid with no userinfo"}
	}
	return nil
}

func containsControl(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0
}

// validateExample accepts only concrete invocations for leaves generated by the
// product scaffold. It keeps examples in help/docs/manifests runnable by
// rejecting unknown commands, missing required names, and extra positionals.
func validateExample(binary string, valueFlags map[string]bool, example string) (string, error) {
	fields := strings.Fields(example)
	if len(fields) == 0 {
		return "", &ValidationError{Message: "scaffold: example must not be empty"}
	}
	if fields[0] != binary {
		return "", &ValidationError{Message: "scaffold: example must start with the generated binary name"}
	}
	if len(fields) < 2 {
		return "", &ValidationError{Message: "scaffold: example must name a generated command"}
	}
	if fields[1] == "update" {
		if err := validateExampleFlagTail(fields[2:], valueFlags, map[string]bool{"check": true}); err != nil {
			return "", &ValidationError{Message: "scaffold: update examples accept only generated flags after the command path"}
		}
		return "update", nil
	}
	if len(fields) < 3 || fields[1] != "widget" {
		return "", &ValidationError{Message: "scaffold: example must name widget list, widget create, widget delete, or update"}
	}
	switch fields[2] {
	case "list":
		if err := validateExampleFlagTail(fields[3:], valueFlags, nil); err != nil {
			return "", &ValidationError{Message: "scaffold: widget list examples accept only generated flags after the command path"}
		}
		return "widget list", nil
	case "create":
		if len(fields) < 4 || strings.HasPrefix(fields[3], "-") {
			return "", &ValidationError{Message: "scaffold: widget create examples must include a positional name before flags"}
		}
		if err := validateExampleFlagTail(fields[4:], valueFlags, nil); err != nil {
			return "", &ValidationError{Message: "scaffold: widget create examples accept only generated flags after the name"}
		}
		return "widget create", nil
	case "delete":
		if len(fields) < 4 || strings.HasPrefix(fields[3], "-") {
			return "", &ValidationError{Message: "scaffold: widget delete examples must include a positional name before flags"}
		}
		if err := validateExampleFlagTail(fields[4:], valueFlags, map[string]bool{"confirm": true}); err != nil {
			return "", &ValidationError{Message: "scaffold: widget delete examples accept only generated flags after the name"}
		}
		return "widget delete", nil
	default:
		return "", &ValidationError{Message: "scaffold: example must name widget list, widget create, widget delete, or update"}
	}
}

// validateExampleFlagTail is a narrow validator for generated examples, not a
// full shell or pflag parser. It allows only generated long flags; value flags
// must use --flag=value or provide the next token as the value.
func validateExampleFlagTail(tokens []string, valueFlags, localBoolFlags map[string]bool) error {
	boolFlags := map[string]bool{
		"json":      true,
		"dry-run":   true,
		"no-prompt": true,
		"quiet":     true,
	}
	for name := range localBoolFlags {
		boolFlags[name] = true
	}
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if !strings.HasPrefix(token, "--") || token == "--" {
			return fmt.Errorf("unexpected positional %q", token)
		}
		name := strings.TrimPrefix(token, "--")
		if before, _, ok := strings.Cut(name, "="); ok {
			name = before
		}
		if valueFlags[name] {
			if strings.Contains(token, "=") {
				continue
			}
			if i+1 >= len(tokens) || strings.HasPrefix(tokens[i+1], "-") {
				return fmt.Errorf("flag %q requires a value", token)
			}
			i++
			continue
		}
		if boolFlags[name] {
			continue
		}
		return fmt.Errorf("unknown flag %q", token)
	}
	return nil
}

func dedupAppend(xs []string, values ...string) []string {
	for _, value := range values {
		found := false
		for _, x := range xs {
			if x == value {
				found = true
				break
			}
		}
		if !found {
			xs = append(xs, value)
		}
	}
	return xs
}

func frameworkGlobalFlagName(name string) bool {
	switch name {
	case "json", "dry-run", "no-prompt", "quiet", "config", "profile", "auth-file", "plain", "jq", "template", "include-meta", "no-color", "no-ansi", "no-pager":
		return true
	default:
		return false
	}
}

// Write generates the project and writes it under dir/<name>, returning the
// project directory. It refuses to overwrite an existing non-empty directory.
func Write(dir string, o Options) (string, error) {
	files, err := Generate(o)
	if err != nil {
		return "", err
	}
	root := filepath.Join(dir, o.Name)
	if entries, err := os.ReadDir(root); err == nil && len(entries) > 0 {
		return "", fmt.Errorf("scaffold: %s already exists and is not empty", root)
	}
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			return "", err
		}
	}
	return root, nil
}
