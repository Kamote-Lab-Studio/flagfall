package themes_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Kamote-Lab-Studio/flagfall/internal/themes"
)

func manifestWith(fields string) fstest.MapFS {
	return fstest.MapFS{"theme.json": {Data: []byte(`{"id":"map","name":"World Map","version":"1.0.0","entry":"dist/index.html","challengeFields":` + fields + `}`)}}
}

func TestChallengeFieldsInManifest(t *testing.T) {
	th, err := themes.Load(manifestWith(`[{"key":"location","type":"location","label":"Map location","required":true},
		{"key":"region","type":"select","label":"Region","options":[{"value":"eu","label":"Europe"}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	if fs := th.Manifest.ChallengeFields; len(fs) != 2 || !fs[0].Required || fs[1].Options[0].Value != "eu" {
		t.Fatalf("fields = %+v", fs)
	}
	for bad, want := range map[string]string{
		`[{"key":"Location","type":"text","label":"L"}]`:                                "lowercase",
		`[{"key":"a","type":"text","label":"A"},{"key":"a","type":"text","label":"B"}]`: "used twice",
		`[{"key":"a","type":"color","label":"A"}]`:                                      "type must be",
		`[{"key":"a","type":"select","label":"A"}]`:                                     "needs options",
		`[{"key":"a","type":"text","label":""}]`:                                        "label is required",
	} {
		if _, err := themes.Load(manifestWith(bad)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", bad, err, want)
		}
	}
}

func TestCheckFields(t *testing.T) {
	fields := []themes.ChallengeField{
		{Key: "location", Type: "location", Label: "Map location", Required: true},
		{Key: "note", Type: "text", Label: "Note"},
		{Key: "floor", Type: "number", Label: "Floor"},
		{Key: "boss", Type: "boolean", Label: "Boss"},
		{Key: "region", Type: "select", Label: "Region", Options: []themes.FieldOption{{Value: "eu", Label: "Europe"}}},
	}
	// YAML gives ints, JSON gives float64: both are numbers.
	ok := themes.CheckFields(fields, map[string]any{
		"location": map[string]any{"city": "Oslo", "lat": 59.9, "lon": 10}, "note": "hi", "floor": 3, "boss": true, "region": "eu",
	})
	if !ok.OK() {
		t.Fatalf("valid values: %+v", ok)
	}

	r := themes.CheckFields(fields, map[string]any{
		"note": 5, "floor": "three", "boss": "yes", "region": "asia", "legacy": 1, "other": "x",
	})
	for _, k := range []string{"note", "floor", "boss", "region"} {
		if r.Invalid[k] == "" {
			t.Errorf("%s not reported: %+v", k, r.Invalid)
		}
	}
	if len(r.Missing) != 1 || r.Missing[0] != "location" || strings.Join(r.Unknown, ",") != "legacy,other" {
		t.Errorf("missing %v unknown %v", r.Missing, r.Unknown)
	}
	if w := r.Warnings(fields); len(w) != 1 || !strings.Contains(w[0], "Map location") {
		t.Errorf("warnings = %v", w)
	}

	for _, loc := range []any{
		map[string]any{"lat": 91.0, "lon": 0.0},
		map[string]any{"lat": 1.0},
		map[string]any{"lat": 1.0, "lon": 2.0, "zoom": 3},
		map[string]any{"lat": 1.0, "lon": 2.0, "city": 7},
		"Oslo",
	} {
		if r := themes.CheckFields(fields, map[string]any{"location": loc}); r.Invalid["location"] == "" {
			t.Errorf("location %v accepted", loc)
		}
	}
	// Empty values count as unset.
	if r := themes.CheckFields(fields, map[string]any{"location": map[string]any{}, "note": "  "}); len(r.Invalid) != 0 || len(r.Missing) != 1 {
		t.Errorf("empty values: %+v", r)
	}
}
