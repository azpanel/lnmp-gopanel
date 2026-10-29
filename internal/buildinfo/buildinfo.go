package buildinfo

// Version and BuiltAt are set by the build scripts using linker flags.
// A plain go run uses the development defaults without inventing a build time.
var (
	Version = "dev"
	BuiltAt = ""
)

type Info struct {
	Version string `json:"version"`
	BuiltAt string `json:"built_at"`
}

func Current() Info {
	return Info{Version: Version, BuiltAt: BuiltAt}
}
