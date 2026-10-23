// Package outputhelp describes command output for help and reference docs.
package outputhelp

import "fmt"

// Descriptions preserves the declared order and leaves custom formats visible.
// The original tokens remain in command annotations and the machine manifest.
func Descriptions(modes []string) []string {
	var descriptions []string
	for _, mode := range modes {
		var description string
		switch mode {
		case "human":
			description = "Shows text you can read in your terminal."
		case "table":
			description = "Shows a table in your terminal."
		case "json":
			description = "Use --json to get JSON for scripts and other programs."
		case "plain":
			description = "Use --plain to get text without color or formatting."
		case "jq":
			description = "Use --jq to filter the JSON output with a jq expression."
		case "template":
			description = "Use --template to format the output with a Go template."
		default:
			description = fmt.Sprintf("Also supports %q output.", mode)
		}
		descriptions = append(descriptions, description)
	}
	return descriptions
}
