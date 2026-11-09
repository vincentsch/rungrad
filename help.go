package rungrad

import (
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	inheritedFlagsCall  = "{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}"
	applicableFlagsCall = "{{rungradGlobalFlagUsages . | trimTrailingWhitespaces}}"
	globalFlagsFuncName = "rungradGlobalFlagUsages"
)

var registerHelpFuncs sync.Once

// useApplicableGlobalFlags makes the Global Flags section of help list only
// the flags a command accepts. --plain, --jq, --template and --include-meta
// are refused by commands without the matching output mode, so their help
// leaves them out. A product that sets its own usage template keeps it.
func useApplicableGlobalFlags(root *cobra.Command) {
	registerHelpFuncs.Do(func() {
		cobra.AddTemplateFunc(globalFlagsFuncName, applicableGlobalFlagUsages)
	})
	if tmpl := root.UsageTemplate(); strings.Contains(tmpl, inheritedFlagsCall) {
		root.SetUsageTemplate(strings.Replace(tmpl, inheritedFlagsCall, applicableFlagsCall, 1))
	}
}

func applicableGlobalFlagUsages(cmd *cobra.Command) string {
	fs := pflag.NewFlagSet(cmd.Name(), pflag.ContinueOnError)
	cmd.InheritedFlags().VisitAll(func(f *pflag.Flag) {
		if flagAppliesTo(cmd, f) {
			fs.AddFlag(f)
		}
	})
	return fs.FlagUsages()
}

// flagAppliesTo reports whether cmd accepts f. Flags without a capability
// annotation apply to every command.
func flagAppliesTo(cmd *cobra.Command, f *pflag.Flag) bool {
	need := f.Annotations[flagCapabilityAnnotation]
	if len(need) == 0 {
		return true
	}
	switch need[0] {
	case capabilityPlain:
		return commandSupportsPlain(cmd)
	case capabilityTransform:
		return commandSupportsTransform(cmd)
	case capabilityMeta:
		return commandSupportsMeta(cmd)
	}
	return true
}
