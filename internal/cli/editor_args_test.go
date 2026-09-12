package cli

import (
	"reflect"
	"testing"
)

func TestEditorArgs(t *testing.T) {
	got, err := editorArgs(`"C:\Program Files\Code\code.exe" --wait 'two words' ""`)
	want := []string{`C:\Program Files\Code\code.exe`, "--wait", "two words", ""}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("%q, %v", got, err)
	}
	for _, v := range []string{`"oops`, `""`, " "} {
		if _, err := editorArgs(v); err == nil {
			t.Fatalf("accepted %q", v)
		}
	}
}
