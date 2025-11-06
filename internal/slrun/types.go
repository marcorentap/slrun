package slrun

type Function struct {
	Name     string `json:"name"`
	BuildDir string `json:"build_dir"`

	imageName   string
	containerId string
	running     bool
	port        int // 127.0.0.1:X->80/tcp
}

type Config struct {
	ConfigFile string
	Functions  []*Function `json:"functions"`
}
