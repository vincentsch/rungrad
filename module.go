package rungrad

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vincentsch/rungrad/internal/cmdtree"
	"github.com/vincentsch/rungrad/manifest"
)

// CommandSpec is one declared catalog row describing a command's user-visible
// contract. It is an independent declaration validated against the built tree by
// App.ValidateCatalog, so mismatches are reported as drift.
type CommandSpec struct {
	// Path is the command path relative to the program name, space-joined, for
	// example "item" or "item list".
	Path string
	// Summary is the expected built command Short text.
	Summary string
	// GroupID is the expected Cobra help group; empty means no help group.
	GroupID string
	// OutputModes are the expected output-mode tokens, in order.
	OutputModes []string
	// Examples are the expected docs/help examples, in order.
	Examples []string
	// Related are the expected related command paths, in order.
	Related []string
	// RequiresAuth is the expected authentication requirement.
	RequiresAuth bool
	// Mutates is the expected state-changing behavior.
	Mutates bool
	// Destructive is the expected destructive behavior. It implies Mutates.
	Destructive bool
	// SupportsMeta is the expected metadata-envelope support.
	SupportsMeta bool
	// Extensions is the expected product-owned namespaced metadata, mirroring
	// rungrad.Command.Extensions for catalog drift detection.
	Extensions manifest.ExtensionSet
}

// FeatureModule is a compiled-in group of commands. Modules are ordinary Go
// values linked into the binary and registered explicitly through App.AddModule;
// rungrad does not load modules at runtime. Each method should return fresh
// values on every call.
type FeatureModule interface {
	Groups() []Group
	Commands() []*Command
	Catalog() []CommandSpec
}

// AddModule registers each module's help groups, top-level commands, and
// catalog rows, in order. Panics on programmer errors such as nil modules,
// conflicting help groups, and reserved command names.
func (a *App) AddModule(modules ...FeatureModule) {
	for i, m := range modules {
		if m == nil {
			panic(fmt.Sprintf("rungrad: feature module at index %d is nil", i))
		}
		for _, g := range m.Groups() {
			a.addGroupChecked(g)
		}
		a.AddCommand(m.Commands()...)
		a.catalog = append(a.catalog, cloneSpecs(m.Catalog())...)
	}
}

// Catalog returns the accumulated declared catalog, sorted by Path, as an
// independent deep copy so callers cannot mutate App state.
func (a *App) Catalog() []CommandSpec {
	out := cloneSpecs(a.catalog)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// ValidateCatalog checks that the declared catalog and the live Cobra tree
// describe the same command surface. It reads live metadata through the same
// commandEntry projection used by manifest emission, so catalog, manifest, and
// generated docs stay tied to one command surface.
func (a *App) ValidateCatalog() error {
	a.markFrameworkCompletion()
	globals := cmdtree.GlobalFlagNames(a.root)

	// Build the live command index from the manifest projection itself. This
	// keeps catalog validation aligned with the data agents and docs already see.
	type liveCommand struct {
		entry   manifest.Command
		groupID string
	}
	live := map[string]liveCommand{}
	var livePaths []string
	for _, cmd := range cmdtree.VisibleCommands(a.root) {
		entry, err := commandEntryChecked(cmd, globals)
		if err != nil {
			return err
		}
		path := strings.Join(entry.Path, " ")
		if path == "" {
			continue
		}
		if _, dup := live[path]; dup {
			return fmt.Errorf("duplicate visible command path %q", path)
		}
		live[path] = liveCommand{entry: entry, groupID: cmd.GroupID}
		livePaths = append(livePaths, path)
	}

	specs := a.Catalog()
	specByPath := map[string]CommandSpec{}
	for _, s := range specs {
		if name := a.reservedPathSegment(s.Path); name != "" {
			return fmt.Errorf("catalog entry %q uses the reserved command name %q", s.Path, name)
		}
		if _, dup := specByPath[s.Path]; dup {
			return fmt.Errorf("duplicate catalog path %q", s.Path)
		}
		specByPath[s.Path] = s
	}

	// Walk a sorted union so missing-command, missing-spec, and per-field drift
	// errors are deterministic even when both sides have different path sets.
	for _, path := range sortedUnion(livePaths, specPaths(specs)) {
		entry, hasLive := live[path]
		spec, hasSpec := specByPath[path]
		switch {
		case hasLive && !hasSpec:
			return fmt.Errorf("visible command %q has no catalog entry", path)
		case hasSpec && !hasLive:
			return fmt.Errorf("catalog entry %q does not resolve to a visible command", path)
		}
		if err := specMatchesEntry(path, spec, entry.entry, entry.groupID); err != nil {
			return err
		}
	}

	if err := validateCommandGroups(a.root); err != nil {
		return err
	}
	return nil
}

// specMatchesEntry compares one declared row with the manifest-shaped live
// command. It intentionally compares ordered slices because examples, related
// commands, and output modes are rendered to users in that order.
func specMatchesEntry(path string, spec CommandSpec, entry manifest.Command, groupID string) error {
	if spec.Summary != entry.Short {
		return fmt.Errorf("command %q summary %q does not match built command short %q",
			path, spec.Summary, entry.Short)
	}
	if spec.GroupID != groupID {
		return fmt.Errorf("command %q group_id=%q does not match built command %q",
			path, spec.GroupID, groupID)
	}
	if !equalStrings(spec.OutputModes, entry.OutputModes) {
		return fmt.Errorf("command %q output modes %v do not match built command %v",
			path, spec.OutputModes, entry.OutputModes)
	}
	if !equalStrings(spec.Examples, entry.Examples) {
		return fmt.Errorf("command %q examples %v do not match built command %v",
			path, spec.Examples, entry.Examples)
	}
	if !equalStrings(spec.Related, entry.Related) {
		return fmt.Errorf("command %q related %v do not match built command %v",
			path, spec.Related, entry.Related)
	}
	if spec.RequiresAuth != entry.RequiresAuth {
		return fmt.Errorf("command %q requires_auth=%t does not match built command %t",
			path, spec.RequiresAuth, entry.RequiresAuth)
	}
	if want := spec.Mutates || spec.Destructive; want != entry.Mutates {
		return fmt.Errorf("command %q mutates=%t does not match built command %t",
			path, want, entry.Mutates)
	}
	if spec.Destructive != entry.Destructive {
		return fmt.Errorf("command %q destructive=%t does not match built command %t",
			path, spec.Destructive, entry.Destructive)
	}
	if spec.SupportsMeta != entry.SupportsMeta {
		return fmt.Errorf("command %q supports_meta=%t does not match built command %t",
			path, spec.SupportsMeta, entry.SupportsMeta)
	}
	// Compare canonical JSON so map order and decoded json.Number values do not
	// create false catalog drift.
	specExt, err := manifest.EncodeExtensions(spec.Extensions)
	if err != nil {
		return fmt.Errorf("command %q has invalid extensions: %w", path, err)
	}
	entryExt, err := manifest.EncodeExtensions(entry.Extensions)
	if err != nil {
		return fmt.Errorf("command %q built extensions are invalid: %w", path, err)
	}
	if specExt != entryExt {
		return fmt.Errorf("command %q extensions %s do not match built command %s",
			path, specExt, entryExt)
	}
	return nil
}

// addGroupChecked gives direct AddGroup and module registration identical
// semantics: repeated identical groups are fine, but a reused id with a
// different title is ambiguous and fails at startup.
func (a *App) addGroupChecked(g Group) {
	if g.ID == "" || g.Title == "" {
		panic("rungrad: help group requires a non-empty id and title")
	}
	for _, existing := range a.root.Groups() {
		if existing.ID == g.ID {
			if existing.Title != g.Title {
				panic(fmt.Sprintf(
					"rungrad: help group %q already registered with title %q, cannot re-register with title %q",
					g.ID, existing.Title, g.Title))
			}
			return
		}
	}
	a.root.AddGroup(&cobra.Group{ID: g.ID, Title: g.Title})
}

// validateCommandTreeNames rejects command names that are hidden from the
// shared visible-tree walk or reserved for framework internals. It walks the
// built Cobra tree so Use strings are checked after Cobra has parsed Name().
func (a *App) validateCommandTreeNames(cmd *cobra.Command) {
	if a.reservedCommandName(cmd.Name()) {
		panic(fmt.Sprintf(
			"rungrad: %q is a reserved framework command name and cannot be registered",
			cmd.Name()))
	}
	for _, sub := range cmd.Commands() {
		a.validateCommandTreeNames(sub)
	}
}

// validateCommandGroups mirrors Cobra's parent-relative group resolution before
// Execute would panic. A subcommand cannot use a group registered only on root;
// the group has to exist on that subcommand's immediate parent.
func validateCommandGroups(parent *cobra.Command) error {
	groups := map[string]bool{}
	for _, g := range parent.Groups() {
		groups[g.ID] = true
	}
	for _, child := range parent.Commands() {
		if child.GroupID != "" && !groups[child.GroupID] {
			return fmt.Errorf("command %q references unregistered help group %q",
				strings.Join(commandPath(child), " "), child.GroupID)
		}
		if err := validateCommandGroups(child); err != nil {
			return err
		}
	}
	return nil
}

// reservedCommandName reports names that either belong to rungrad internals or
// are filtered out of the visible command tree shared by manifests and docs.
func (a *App) reservedCommandName(name string) bool {
	switch {
	case name == "help":
		return true
	case name == manifestCommandName:
		return a.manifestEndpointMode == ManifestEndpointRungradOwned ||
			a.manifestEndpointMode == ManifestEndpointHostRendered
	case a.manifestEndpointName != "" && name == a.manifestEndpointName:
		return true
	case name == "completion":
		return a.completionSurface != SurfaceHostOwned
	}
	return false
}

// reservedPathSegment applies the same reserved-name rule to catalog paths,
// where each space-separated field is one command path segment.
func (a *App) reservedPathSegment(path string) string {
	for _, field := range strings.Fields(path) {
		if a.reservedCommandName(field) {
			return field
		}
	}
	return ""
}

// markFrameworkCompletion annotates Cobra's generated completion command after
// it appears, while leaving host-owned completion commands visible.
func (a *App) markFrameworkCompletion() {
	if a.completionSurface == SurfaceHostOwned {
		return
	}
	for _, c := range a.root.Commands() {
		if c.Name() != "completion" {
			continue
		}
		if c.Annotations == nil {
			c.Annotations = map[string]string{}
		}
		// Cobra's generated command is indistinguishable by name from a product
		// command, so projection filters by this annotation instead of "completion".
		c.Annotations[cmdtree.AnnotationFrameworkCompletion] = "true"
		return
	}
}

// cloneSpecs protects App catalog state from caller mutation. The slice fields
// need their own copies because CommandSpec is otherwise a shallow value.
func cloneSpecs(in []CommandSpec) []CommandSpec {
	out := make([]CommandSpec, len(in))
	for i, s := range in {
		out[i] = s
		out[i].OutputModes = append([]string(nil), s.OutputModes...)
		out[i].Examples = append([]string(nil), s.Examples...)
		out[i].Related = append([]string(nil), s.Related...)
		out[i].Extensions = cloneExtensions(s.Extensions)
	}
	return out
}

// catalogRefIdent identifies pointer-like values while cloning catalog
// extensions. Slice views include length and capacity to avoid false cycle
// matches between a slice and one of its sub-slices.
type catalogRefIdent struct {
	kind     reflect.Kind
	typ      reflect.Type
	ptr      uintptr
	length   int
	capacity int
}

// cloneExtensions copies the product-owned metadata on catalog rows. Catalog
// rows are returned to callers, so nested extension containers must not share
// mutable state with App.catalog.
func cloneExtensions(in manifest.ExtensionSet) manifest.ExtensionSet {
	if in == nil {
		return nil
	}
	out := make(manifest.ExtensionSet, len(in))
	for ns, obj := range in {
		if obj == nil {
			out[ns] = nil
			continue
		}
		cloned := make(manifest.ExtensionObject, len(obj))
		for field, value := range obj {
			cloned[field] = cloneExtensionValueAny(value)
		}
		out[ns] = cloned
	}
	return out
}

// cloneExtensionValueAny starts a new cycle-tracking walk for one extension
// field value.
func cloneExtensionValueAny(value any) any {
	if value == nil {
		return nil
	}
	return cloneExtensionValue(reflect.ValueOf(value), map[catalogRefIdent]bool{}).Interface()
}

// cloneExtensionValue preserves typed containers such as []string and
// map[string][]string instead of converting extensions through JSON. Invalid
// cyclic values panic here during module registration, before they can cause a
// stack overflow in later validation or catalog reads.
func cloneExtensionValue(rv reflect.Value, onPath map[catalogRefIdent]bool) reflect.Value {
	if !rv.IsValid() {
		return rv
	}
	for rv.Kind() == reflect.Interface {
		if rv.IsNil() {
			return reflect.Zero(rv.Type())
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Pointer:
		if rv.IsNil() {
			return reflect.Zero(rv.Type())
		}
		enterCatalogRef(rv, onPath)
		defer leaveCatalogRef(rv, onPath)
		out := reflect.New(rv.Type().Elem())
		out.Elem().Set(cloneExtensionValue(rv.Elem(), onPath))
		return out
	case reflect.Map:
		if rv.IsNil() {
			return reflect.Zero(rv.Type())
		}
		enterCatalogRef(rv, onPath)
		defer leaveCatalogRef(rv, onPath)
		out := reflect.MakeMapWithSize(rv.Type(), rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), cloneExtensionValue(iter.Value(), onPath))
		}
		return out
	case reflect.Slice:
		if rv.IsNil() {
			return reflect.Zero(rv.Type())
		}
		enterCatalogRef(rv, onPath)
		defer leaveCatalogRef(rv, onPath)
		out := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Cap())
		for i := 0; i < rv.Len(); i++ {
			out.Index(i).Set(cloneExtensionValue(rv.Index(i), onPath))
		}
		return out
	case reflect.Array:
		out := reflect.New(rv.Type()).Elem()
		for i := 0; i < rv.Len(); i++ {
			out.Index(i).Set(cloneExtensionValue(rv.Index(i), onPath))
		}
		return out
	default:
		return rv
	}
}

// enterCatalogRef tracks only the active recursion path, so shared acyclic
// values clone successfully while values that point back to an ancestor panic.
func enterCatalogRef(rv reflect.Value, onPath map[catalogRefIdent]bool) {
	id := catalogRefKey(rv)
	if id.ptr == 0 {
		return
	}
	if onPath[id] {
		panic("rungrad: catalog extension value contains a cycle")
	}
	onPath[id] = true
}

func leaveCatalogRef(rv reflect.Value, onPath map[catalogRefIdent]bool) {
	id := catalogRefKey(rv)
	if id.ptr != 0 {
		delete(onPath, id)
	}
}

// catalogRefKey mirrors the extension validator's reference identity. Slice
// length and capacity keep sub-slices distinct from their parents.
func catalogRefKey(rv reflect.Value) catalogRefIdent {
	id := catalogRefIdent{kind: rv.Kind(), typ: rv.Type(), ptr: rv.Pointer()}
	if rv.Kind() == reflect.Slice {
		id.length = rv.Len()
		id.capacity = rv.Cap()
	}
	return id
}

// equalStrings treats nil and empty slices as equal while preserving order for
// non-empty values.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func specPaths(specs []CommandSpec) []string {
	out := make([]string, len(specs))
	for i, spec := range specs {
		out[i] = spec.Path
	}
	return out
}

// sortedUnion returns each path once in lexical order so validation reports the
// same first drift across runs.
func sortedUnion(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, path := range a {
		if !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	for _, path := range b {
		if !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}
