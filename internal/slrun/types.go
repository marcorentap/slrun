package slrun

type Function struct {
	Name     string `json:"name"`
	BuildDir string `json:"build_dir"`

	imageName   string
	containerId string
	running     bool
}

type Config struct {
	ConfigFile string
	Functions  []*Function `json:"functions"`
}
