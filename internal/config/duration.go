package config

import (
	"fmt"
	"time"
)

// Duration is a time.Duration that unmarshals from a Go duration string
// ("35ms", "2s", "1m30s") so config files stay readable.
type Duration time.Duration

// D returns the value as a time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

// UnmarshalYAML implements yaml.Unmarshaler.
func (d *Duration) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	if s == "" {
		*d = 0
		return nil
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalYAML implements yaml.Marshaler so round-tripped config stays legible.
func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }
