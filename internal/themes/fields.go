package themes

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// ChallengeField is an extra per-challenge value a theme uses, such as a
// map location. Challenges keep them in theme_fields; the admin form shows
// the active theme's fields only.
type ChallengeField struct {
	Key      string        `json:"key"`
	Type     string        `json:"type"` // text, number, boolean, select, location
	Label    string        `json:"label"`
	Help     string        `json:"help,omitempty"`
	Required bool          `json:"required,omitempty"`
	Options  []FieldOption `json:"options,omitempty"`
}

// FieldOption is one choice of a select field.
type FieldOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// MaxFieldText is the longest text value a challenge field takes.
const MaxFieldText = 500

var fieldKeyRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// checkFields validates a manifest's challengeFields.
func checkFields(fields []ChallengeField) error {
	seen := map[string]bool{}
	for i, f := range fields {
		where := fmt.Sprintf("challengeFields[%d]", i)
		if !fieldKeyRE.MatchString(f.Key) {
			return fmt.Errorf("%s: key %q must be lowercase letters, digits and _", where, f.Key)
		}
		if seen[f.Key] {
			return fmt.Errorf("%s: key %q is used twice", where, f.Key)
		}
		seen[f.Key] = true
		if strings.TrimSpace(f.Label) == "" {
			return fmt.Errorf("%s: label is required", where)
		}
		switch f.Type {
		case "text", "number", "boolean", "location":
			if len(f.Options) > 0 {
				return fmt.Errorf("%s: only select fields have options", where)
			}
		case "select":
			if len(f.Options) == 0 {
				return fmt.Errorf("%s: a select field needs options", where)
			}
		default:
			return fmt.Errorf("%s: type must be text, number, boolean, select or location", where)
		}
	}
	return nil
}

// FieldReport is the result of checking a challenge's theme_fields.
type FieldReport struct {
	// Invalid maps a field key to why its value can't be used.
	Invalid map[string]string
	// Missing lists required fields with no value.
	Missing []string
	// Unknown lists keys the theme doesn't use. They're kept (another
	// theme may use them) but players of this theme won't see them.
	Unknown []string
}

// OK reports whether nothing is wrong at all.
func (r FieldReport) OK() bool {
	return len(r.Invalid) == 0 && len(r.Missing) == 0 && len(r.Unknown) == 0
}

// Warnings describes the missing required fields in sentences. Unknown
// keys aren't warned about one challenge at a time: a pack written for
// another theme would repeat the same line for every challenge.
func (r FieldReport) Warnings(fields []ChallengeField) []string {
	label := map[string]string{}
	for _, f := range fields {
		label[f.Key] = f.Label
	}
	var w []string
	for _, k := range r.Missing {
		w = append(w, fmt.Sprintf("%q (%s) is required by the active theme but not set.", label[k], k))
	}
	return w
}

// CheckFields compares challenge theme_fields values with a theme's fields.
func CheckFields(fields []ChallengeField, values map[string]any) FieldReport {
	r := FieldReport{Invalid: map[string]string{}}
	known := map[string]bool{}
	for _, f := range fields {
		known[f.Key] = true
		v, set := values[f.Key]
		if !set || isEmpty(v) {
			if f.Required {
				r.Missing = append(r.Missing, f.Key)
			}
			continue
		}
		if msg := checkValue(f, v); msg != "" {
			r.Invalid[f.Key] = msg
		}
	}
	for k := range values {
		if !known[k] {
			r.Unknown = append(r.Unknown, k)
		}
	}
	sort.Strings(r.Unknown)
	return r
}

func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	case map[string]any:
		return len(x) == 0
	}
	return false
}

func checkValue(f ChallengeField, v any) string {
	switch f.Type {
	case "text":
		s, ok := v.(string)
		if !ok {
			return "Must be text."
		}
		if len(s) > MaxFieldText {
			return fmt.Sprintf("Use at most %d characters.", MaxFieldText)
		}
	case "number":
		if _, ok := number(v); !ok {
			return "Must be a number."
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return "Must be true or false."
		}
	case "select":
		s, _ := v.(string)
		for _, o := range f.Options {
			if o.Value == s {
				return ""
			}
		}
		vals := make([]string, len(f.Options))
		for i, o := range f.Options {
			vals[i] = o.Value
		}
		return "Must be one of: " + strings.Join(vals, ", ") + "."
	case "location":
		m, ok := v.(map[string]any)
		if !ok {
			return "Must be a location: {lat, lon} and optionally a city."
		}
		lat, okLat := number(m["lat"])
		lon, okLon := number(m["lon"])
		if !okLat || !okLon || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
			return "Needs lat (-90 to 90) and lon (-180 to 180)."
		}
		for k, x := range m {
			switch k {
			case "lat", "lon":
			case "city":
				if s, ok := x.(string); !ok || len(s) > 80 {
					return "city must be text (at most 80 characters)."
				}
			default:
				return fmt.Sprintf("Unexpected %q; a location has lat, lon and city.", k)
			}
		}
	}
	return ""
}

// number accepts the numeric types JSON and YAML decoders produce.
func number(v any) (float64, bool) {
	var f float64
	switch x := v.(type) {
	case float64:
		f = x
	case float32:
		f = float64(x)
	case int:
		f = float64(x)
	case int64:
		f = float64(x)
	case uint64:
		f = float64(x)
	default:
		return 0, false
	}
	return f, !math.IsNaN(f) && !math.IsInf(f, 0)
}
