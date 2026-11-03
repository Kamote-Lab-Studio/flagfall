package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunCommands(t *testing.T) {
	cases := []struct {
		args     []string
		code     int
		contains string
	}{
		{nil, 2, "Usage:"},
		{[]string{"help"}, 0, "Commands:"},
		{[]string{"version"}, 0, "flagfall "},
		{[]string{"bogus"}, 2, `unknown command "bogus"`},
	}
	for _, c := range cases {
		var out, errOut bytes.Buffer
		code := run(c.args, &out, &errOut)
		if code != c.code {
			t.Errorf("run(%v) = %d, want %d", c.args, code, c.code)
		}
		if got := out.String() + errOut.String(); !strings.Contains(got, c.contains) {
			t.Errorf("run(%v) output %q does not contain %q", c.args, got, c.contains)
		}
	}
}
