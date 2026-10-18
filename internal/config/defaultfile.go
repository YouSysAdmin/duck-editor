package config

import _ "embed" // the documented default config

// DefaultFile is config.yaml with every option at its default value and
// a comment for each, what dk export config writes.
//
//go:embed default.yaml
var DefaultFile string
