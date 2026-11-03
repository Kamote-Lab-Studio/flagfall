package themes

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Setting is one organizer-editable value declared by a theme.
type Setting struct {
	Key          string          `json:"key"`
	Type         string          `json:"type"` // color, text, boolean, number, select, font, image
	Label        string          `json:"label"`
	Help         string          `json:"help,omitempty"`
	Group        string          `json:"group,omitempty"`
	Default      any             `json:"default"`
	CSSVar       string          `json:"cssVar,omitempty"`
	ContrastWith string          `json:"contrastWith,omitempty"`
	MinContrast  float64         `json:"minContrast,omitempty"`
	MaxLength    int             `json:"maxLength,omitempty"`
	Multiline    bool            `json:"multiline,omitempty"`
	Markdown     bool            `json:"markdown,omitempty"`
	Min          *float64        `json:"min,omitempty"`
	Max          *float64        `json:"max,omitempty"`
	Step         *float64        `json:"step,omitempty"`
	Options      json.RawMessage `json:"options,omitempty"` // select: [{value,label}], font: ["Family"]
	Accept       []string        `json:"accept,omitempty"`
	MaxBytes     int64           `json:"maxBytes,omitempty"`
}

// SelectOptions returns a select setting's choices.
func (s Setting) SelectOptions() []FieldOption {
	var out []FieldOption
	_ = json.Unmarshal(s.Options, &out)
	return out
}

// FontOptions returns a font setting's families.
func (s Setting) FontOptions() []string {
	var out []string
	_ = json.Unmarshal(s.Options, &out)
	return out
}

// DefaultMinContrast is WCAG AA for body text.
const DefaultMinContrast = 4.5

// MaxImageBytes caps image settings (logo, favicon, backgrounds).
const MaxImageBytes = 2 << 20

// ImageTypes are the image formats a theme setting may accept.
var ImageTypes = []string{"image/png", "image/svg+xml", "image/webp", "image/jpeg"}

var (
	themeIDRE    = regexp.MustCompile(`^[a-z][a-z0-9-]{1,47}$`)
	semverRE     = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)
	settingKeyRE = regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)
	color6RE     = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
)

// RequiredPages are the routes every theme must implement.
var RequiredPages = []string{"board", "challenge", "scoreboard", "teams", "team", "page",
	"signIn", "signUp", "account", "announcements", "notStarted", "over"}

// Validate checks the manifest against the rules in
// docs/dev/theme-manifest.md that the server relies on.
func (m *Manifest) Validate() error {
	if m.ManifestVersion != 0 && m.ManifestVersion != 1 {
		return fmt.Errorf("manifestVersion %d isn't supported (this server reads 1)", m.ManifestVersion)
	}
	if !themeIDRE.MatchString(m.ID) {
		return fmt.Errorf("id %q must be 2–48 lowercase letters, digits and dashes, starting with a letter", m.ID)
	}
	if n := utf8.RuneCountInString(m.Name); n == 0 || n > 60 {
		return fmt.Errorf("name must be 1–60 characters")
	}
	if utf8.RuneCountInString(m.Description) > 280 {
		return fmt.Errorf("description must be at most 280 characters")
	}
	if !semverRE.MatchString(m.Version) {
		return fmt.Errorf("version %q must be a semantic version such as 1.0.0", m.Version)
	}
	if m.Pages != nil {
		var missing []string
		for _, p := range RequiredPages {
			if _, ok := m.Pages[p]; !ok {
				missing = append(missing, p)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("pages is missing %s", strings.Join(missing, ", "))
		}
	}
	return checkSettings(m.Settings)
}

func checkSettings(settings []Setting) error {
	seen := map[string]Setting{}
	for i, s := range settings {
		where := fmt.Sprintf("settings[%d]", i)
		if !settingKeyRE.MatchString(s.Key) {
			return fmt.Errorf("%s: key %q must start with a lowercase letter and use only letters and digits", where, s.Key)
		}
		if _, dup := seen[s.Key]; dup {
			return fmt.Errorf("%s: key %q is used twice", where, s.Key)
		}
		seen[s.Key] = s
		if strings.TrimSpace(s.Label) == "" {
			return fmt.Errorf("%s: label is required", where)
		}
		if s.CSSVar != "" && !cssVarName.MatchString(s.CSSVar) {
			return fmt.Errorf("%s: cssVar %q must look like --name", where, s.CSSVar)
		}
		switch s.Type {
		case "color":
			if d, _ := s.Default.(string); !color6RE.MatchString(d) {
				return fmt.Errorf("%s: a color default must look like #RRGGBB", where)
			}
		case "text":
			if _, ok := s.Default.(string); !ok {
				return fmt.Errorf("%s: a text default must be a string", where)
			}
			if s.MaxLength < 0 || s.MaxLength > 2000 {
				return fmt.Errorf("%s: maxLength must be 1–2000", where)
			}
		case "boolean":
			if _, ok := s.Default.(bool); !ok {
				return fmt.Errorf("%s: a boolean default must be true or false", where)
			}
		case "number":
			d, ok := s.Default.(float64)
			if !ok {
				return fmt.Errorf("%s: a number default must be a number", where)
			}
			if (s.Min != nil && d < *s.Min) || (s.Max != nil && d > *s.Max) {
				return fmt.Errorf("%s: the default is outside min and max", where)
			}
		case "select":
			opts := s.SelectOptions()
			if len(opts) < 2 {
				return fmt.Errorf("%s: a select setting needs at least 2 options", where)
			}
			d, _ := s.Default.(string)
			if !hasOption(opts, d) {
				return fmt.Errorf("%s: the default %q isn't one of the options", where, d)
			}
		case "font":
			opts := s.FontOptions()
			if len(opts) == 0 {
				return fmt.Errorf("%s: a font setting needs options", where)
			}
			d, _ := s.Default.(string)
			if !hasString(opts, d) {
				return fmt.Errorf("%s: the default %q isn't one of the options", where, d)
			}
			for _, f := range opts {
				if !safeValue.MatchString(f) {
					return fmt.Errorf("%s: font name %q has characters that aren't allowed", where, f)
				}
			}
		case "image":
			for _, a := range s.Accept {
				if !hasString(ImageTypes, a) {
					return fmt.Errorf("%s: accept may only list %s", where, strings.Join(ImageTypes, ", "))
				}
			}
			if s.MaxBytes < 0 || s.MaxBytes > MaxImageBytes {
				return fmt.Errorf("%s: maxBytes must be at most %d", where, MaxImageBytes)
			}
			if s.CSSVar != "" {
				return fmt.Errorf("%s: image settings can't set a cssVar", where)
			}
		default:
			return fmt.Errorf("%s: type must be color, text, boolean, number, select, font or image", where)
		}
	}
	for i, s := range settings {
		if s.ContrastWith == "" {
			continue
		}
		other, ok := seen[s.ContrastWith]
		if s.Type != "color" || !ok || other.Type != "color" {
			return fmt.Errorf("settings[%d]: contrastWith must name another color setting", i)
		}
	}
	return nil
}

func hasOption(opts []FieldOption, v string) bool {
	for _, o := range opts {
		if o.Value == v {
			return true
		}
	}
	return false
}

func hasString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// Resolve merges stored values over the manifest defaults, dropping keys the
// theme doesn't declare.
func Resolve(settings []Setting, values map[string]any) map[string]any {
	out := make(map[string]any, len(settings))
	for _, s := range settings {
		if v, ok := values[s.Key]; ok {
			out[s.Key] = v
		} else if s.Default != nil {
			out[s.Key] = s.Default
		}
	}
	return out
}

// CheckValues validates organizer values against the theme's settings. It
// returns the cleaned values (unknown keys dropped) and, per key, why a
// value can't be saved. imageOK reports whether an image value names an
// uploaded file.
func CheckValues(settings []Setting, values map[string]any, imageOK func(string) bool) (map[string]any, map[string]string) {
	clean := map[string]any{}
	bad := map[string]string{}
	byKey := map[string]Setting{}
	for _, s := range settings {
		byKey[s.Key] = s
	}
	for k, v := range values {
		s, ok := byKey[k]
		if !ok {
			continue
		}
		if v == nil {
			continue // back to the default
		}
		if msg := checkSettingValue(s, v, imageOK); msg != "" {
			bad[k] = msg
			continue
		}
		clean[k] = v
	}
	merged := Resolve(settings, clean)
	for _, s := range settings {
		if s.ContrastWith == "" || bad[s.Key] != "" || bad[s.ContrastWith] != "" {
			continue
		}
		fg, _ := merged[s.Key].(string)
		bg, _ := merged[s.ContrastWith].(string)
		min := s.MinContrast
		if min <= 0 {
			min = DefaultMinContrast
		}
		if ratio, ok := ContrastRatio(fg, bg); ok && ratio < min {
			bad[s.Key] = fmt.Sprintf("Contrast with %s is %.1f:1; it must be at least %.1f:1 to stay readable.",
				labelOf(byKey[s.ContrastWith]), math.Floor(ratio*10)/10, min)
		}
	}
	return clean, bad
}

func labelOf(s Setting) string {
	if s.Label != "" {
		return s.Label
	}
	return s.Key
}

func checkSettingValue(s Setting, v any, imageOK func(string) bool) string {
	switch s.Type {
	case "color":
		c, ok := v.(string)
		if !ok || !color6RE.MatchString(c) {
			return "Use a color like #FFB000."
		}
	case "text":
		t, ok := v.(string)
		if !ok {
			return "Must be text."
		}
		max := s.MaxLength
		if max == 0 {
			max = 2000
		}
		if utf8.RuneCountInString(t) > max {
			return fmt.Sprintf("Use at most %d characters.", max)
		}
		if !s.Multiline && strings.ContainsAny(t, "\r\n") {
			return "Use a single line."
		}
		if s.CSSVar != "" && t != "" && !safeCSSValue(t) {
			return "Use letters, digits, spaces and simple punctuation only."
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return "Must be on or off."
		}
	case "number":
		n, ok := v.(float64)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			return "Must be a number."
		}
		if s.Min != nil && n < *s.Min {
			return "Must be at least " + strconv.FormatFloat(*s.Min, 'f', -1, 64) + "."
		}
		if s.Max != nil && n > *s.Max {
			return "Must be at most " + strconv.FormatFloat(*s.Max, 'f', -1, 64) + "."
		}
	case "select":
		o, ok := v.(string)
		if !ok || !hasOption(s.SelectOptions(), o) {
			return "Pick one of the options."
		}
	case "font":
		o, ok := v.(string)
		if !ok || !hasString(s.FontOptions(), o) {
			return "Pick one of the fonts."
		}
	case "image":
		o, ok := v.(string)
		if !ok {
			return "Upload an image."
		}
		if o != "" && (imageOK == nil || !imageOK(o)) {
			return "That image isn't uploaded."
		}
	default:
		return "This setting can't be changed."
	}
	return ""
}

func safeCSSValue(v string) bool {
	return safeValue.MatchString(v) && strings.Count(v, `"`)%2 == 0 && strings.Count(v, `'`)%2 == 0
}

// ContrastRatio is the WCAG 2 contrast ratio of two #RRGGBB colors.
func ContrastRatio(a, b string) (float64, bool) {
	la, ok1 := luminance(a)
	lb, ok2 := luminance(b)
	if !ok1 || !ok2 {
		return 0, false
	}
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05), true
}

func luminance(hex string) (float64, bool) {
	if !color6RE.MatchString(hex) {
		return 0, false
	}
	var c [3]float64
	for i := range 3 {
		n, _ := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		v := float64(n) / 255
		if v <= 0.04045 {
			c[i] = v / 12.92
		} else {
			c[i] = math.Pow((v+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2], true
}
