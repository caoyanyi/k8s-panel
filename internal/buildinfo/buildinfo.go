package buildinfo

import "fmt"

var (
	Version = "dev"
	Commit  = "unknown"
)

func String(name string) string {
	return fmt.Sprintf("%s %s (commit %s)", name, Version, Commit)
}
