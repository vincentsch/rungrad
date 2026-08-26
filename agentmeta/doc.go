// Package agentmeta projects a rungrad manifest into an agent metadata document.
//
// The projection is pure: it reads a validated rungrad-manifest/1 value and
// derives inventory and non-executing command guidance for repository-scoped
// skills, local stdio MCP adapters, and hosted MCP providers owned by product
// code. It does not execute commands, read runtime configuration, load
// credentials, contact networks, or generate MCP tool definitions.
package agentmeta
