package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// JSONMap is a free-form object persisted to a MySQL JSON column.
type JSONMap map[string]any

func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

func (m *JSONMap) Scan(src any) error {
	if src == nil {
		*m = nil
		return nil
	}
	b, ok := src.([]byte)
	if !ok {
		s, isString := src.(string)
		if !isString {
			return fmt.Errorf("models: cannot scan %T into JSONMap", src)
		}
		b = []byte(s)
	}
	if len(b) == 0 {
		*m = nil
		return nil
	}
	return json.Unmarshal(b, m)
}

// StringSlice persists a list of short strings (keywords, tags) as JSON.
type StringSlice []string

func (s StringSlice) Value() (driver.Value, error) {
	if s == nil {
		return "[]", nil
	}
	return json.Marshal(s)
}

func (s *StringSlice) Scan(src any) error {
	if src == nil {
		*s = nil
		return nil
	}
	b, ok := src.([]byte)
	if !ok {
		str, isString := src.(string)
		if !isString {
			return fmt.Errorf("models: cannot scan %T into StringSlice", src)
		}
		b = []byte(str)
	}
	if len(b) == 0 {
		*s = nil
		return nil
	}
	return json.Unmarshal(b, s)
}
