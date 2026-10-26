package xiaohongshu

import "testing"

func TestExactRedIDMatches(t *testing.T) {
	items := []UserSearchItem{{RedID: "123"}, {RedID: "1234"}, {RedID: "123"}}
	got := exactRedIDMatches(items, "123")
	if len(got) != 2 {
		t.Fatalf("got %d matches", len(got))
	}
}

func TestRandomConfirmationToken(t *testing.T) {
	a, err := randomConfirmationToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := randomConfirmationToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 32 || a == b {
		t.Fatalf("unexpected tokens %q %q", a, b)
	}
}
