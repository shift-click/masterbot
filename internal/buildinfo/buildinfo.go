package buildinfo

import "os"

const (
	defaultVersion   = "dev"
	defaultRevision  = "unknown"
	defaultBuildTime = "unknown"
)

type Info struct {
	Version   string
	Revision  string
	BuildTime string
}

func FromEnv() Info {
	return Info{
		Version:   firstNonEmpty(os.Getenv("JUCOBOT_BUILD_VERSION"), defaultVersion),
		Revision:  firstNonEmpty(os.Getenv("JUCOBOT_BUILD_REVISION"), defaultRevision),
		BuildTime: firstNonEmpty(os.Getenv("JUCOBOT_BUILD_TIME"), defaultBuildTime),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
