package version

import "fmt"

var (
	Version = "dev"
	Commit  = "unknown"
	BuiltAt = "unknown"
)

type Info struct {
	Version string
	Commit  string
	BuiltAt string
}

func Current() Info {
	return Info{
		Version: Version,
		Commit:  Commit,
		BuiltAt: BuiltAt,
	}
}

func (info Info) String() string {
	return fmt.Sprintf(
		"sealbuild %s\ncommit: %s\nbuilt: %s\n",
		info.Version,
		info.Commit,
		info.BuiltAt,
	)
}
