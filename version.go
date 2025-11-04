package blades

import "runtime/debug"

var (
	// Version is the current blades version.
	Version = buildVersion("github.com/go-kratos/blades")
)

// buildVersion retrieves the version of the specified module path from build info.
func buildVersion(path string) string {
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, d := range buildInfo.Deps {
		if d.Path == path {
			if d.Replace != nil {
				return d.Replace.Version
			}
			return d.Version
		}
	}
	return ""
}
