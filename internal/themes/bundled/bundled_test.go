package bundled_test

import (
	"testing"

	"github.com/Kamote-Lab-Studio/flagfall/internal/themes"
	"github.com/Kamote-Lab-Studio/flagfall/internal/themes/bundled"
)

// Every bundled theme's manifest is valid and declares every required page.
func TestBundledThemesLoad(t *testing.T) {
	list, err := bundled.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 2 || list[0].Manifest.ID != themes.DefaultID {
		t.Fatalf("bundled = %d themes, first %q", len(list), list[0].Manifest.ID)
	}
	for _, th := range list {
		for _, p := range themes.RequiredPages {
			if th.Manifest.Pages[p] == "" {
				t.Errorf("%s: page %q has no path", th.Manifest.ID, p)
			}
		}
	}
}
