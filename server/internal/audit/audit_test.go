package audit

import (
	"strings"
	"testing"
)

func TestScrub(t *testing.T) {
	out := string(Scrub([]byte(`{"username":"a","password":"p@ss","nested":{"apiToken":"t","list":[{"pin":"1234"}]},"amountCents":5}`)))
	for _, leak := range []string{"p@ss", `"t"`, "1234"} {
		if strings.Contains(out, leak) {
			t.Fatalf("leaked %s in %s", leak, out)
		}
	}
	if !strings.Contains(out, `"amountCents":5`) || !strings.Contains(out, `"username":"a"`) {
		t.Fatalf("over-scrubbed: %s", out)
	}
}
