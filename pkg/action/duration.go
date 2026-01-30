package action

import (
	"encoding/json"
	"errors"
	"time"
)

// MarshalJSON implements json.Marshaler for Duration.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// UnmarshalJSON implements json.Unmarshaler for Duration.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		*d = Duration(time.Duration(value))
		return nil
	case string:
		tmp, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		*d = Duration(tmp)
		return nil
	default:
		return errors.New("invalid duration")
	}
}

// MarshalYAML implements yaml.Marshaler for Duration.
func (d Duration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}

// UnmarshalYAML implements yaml.Unmarshaler for Duration.
func (d *Duration) UnmarshalYAML(unmarshal func(any) error) error {
	// First, try to unmarshal as an int64 (nanoseconds)
	var n int64
	if err := unmarshal(&n); err == nil {
		*d = Duration(time.Duration(n))
		return nil
	}

	// Otherwise, try as a string (duration format like "1s", "5m")
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	tmp, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(tmp)
	return nil
}

// String returns the string representation of the duration.
func (d Duration) String() string {
	return time.Duration(d).String()
}

// AsDuration returns the underlying time.Duration.
func (d Duration) AsDuration() time.Duration {
	return time.Duration(d)
}
