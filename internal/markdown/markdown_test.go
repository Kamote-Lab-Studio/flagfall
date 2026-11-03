package markdown

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	out := Render("**bold** and `code`\n\n```py\nprint(1)\n```\n\n[link](https://example.org)")
	for _, want := range []string{"<strong>bold</strong>", "<code>code</code>", `class="language-py"`, `rel="nofollow noopener"`, `target="_blank"`} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestStripsDangerousHTML(t *testing.T) {
	cases := []string{
		"<script>alert(1)</script>",
		`<img src=x onerror="alert(1)">`,
		"[x](javascript:alert(1))",
		`<iframe src="https://evil.example"></iframe>`,
	}
	for _, in := range cases {
		out := strings.ToLower(Render(in))
		for _, bad := range []string{"<script", "onerror", "javascript:", "<iframe"} {
			if strings.Contains(out, bad) {
				t.Errorf("Render(%q) kept %q: %s", in, bad, out)
			}
		}
	}
}
