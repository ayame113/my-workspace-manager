// Package skills embeds the agent skills shipped with ws so that the binary can install them.
package skills

import "embed"

//go:embed ws-task
var FS embed.FS
