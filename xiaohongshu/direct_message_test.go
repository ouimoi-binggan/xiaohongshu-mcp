package xiaohongshu

import (
	"testing"
)

func TestExactRedIDMatches(t *testing.T) {
	items := []UserSearchItem{{RedID: "123"}, {RedID: "1234"}, {RedID: "123"}}
	got := exactRedIDMatches(items, "123")
	if len(got) != 2 {
		t.Fatalf("got %d matches", len(got))
	}
}

func TestRecipientFromResolved(t *testing.T) {
	got, err := recipientFromResolved("95412119463", ResolvedRecipient{
		UserID: "6765a403000000001801d181",
		Nickname: "waffle",
		ProfileURL: "https://www.xiaohongshu.com/user/profile/6765a403000000001801d181?xsec_token=T",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.RedID != "95412119463" || got.XsecToken != "T" {
		t.Fatalf("unexpected recipient: %#v", got)
	}
}

func TestRecipientFromResolvedRejectsMismatch(t *testing.T) {
	_, err := recipientFromResolved("95412119463", ResolvedRecipient{
		UserID: "abc",
		ProfileURL: "https://www.xiaohongshu.com/user/profile/def",
	})
	if err == nil {
		t.Fatal("expected mismatch error")
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
