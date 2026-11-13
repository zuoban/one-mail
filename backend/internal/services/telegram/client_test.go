package telegram

import "testing"

func TestStripHTMLAndDecodeEntities(t *testing.T) {
	rawHTML := `<style>.a { color: red; }</style><div>Hello&nbsp;World&#33;<br/>Line&#x20;2</div><p>End</p>`
	got := stripHTML(rawHTML)
	want := "Hello World!\nLine 2\nEnd"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}

	escaped := escapeMarkdown("v1.0_test!")
	if escaped != `v1\.0\_test\!` {
		t.Fatalf("unexpected markdown escape: %q", escaped)
	}
}
