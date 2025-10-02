package log

// Config defines basic logger settings.
type Config struct {
	Level    string `json:"level" yaml:"level"`
	Encoding string `json:"encoding" yaml:"encoding"`
	Output   string `json:"output" yaml:"output"`
}
