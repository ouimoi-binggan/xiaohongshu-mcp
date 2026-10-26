package xiaohongshu

import (
	"strings"
	"testing"
)

func TestParseUserCardLines(t *testing.T) {
	nick, redID := parseUserCardLines([]string{"南瓜妹妹", "小红书号：95412119463", "12篇笔记"})
	if nick != "南瓜妹妹" || redID != "95412119463" {
		t.Fatalf("got nickname=%q redID=%q", nick, redID)
	}
}

func TestMakeUserSearchURL(t *testing.T) {
	got := makeUserSearchURL("954 号")
	for _, want := range []string{"keyword=954+%E5%8F%B7", "source=web_search_result_users", "target_search=users", "type=user"} {
		if !strings.Contains(got, want) {
			t.Fatalf("URL %q missing %q", got, want)
		}
	}
}

func TestCompactLines(t *testing.T) {
	got := compactLines("  A \n\n 小红书号: 123 \r\n")
	if len(got) != 2 || got[0] != "A" || got[1] != "小红书号: 123" {
		t.Fatalf("unexpected lines: %#v", got)
	}
}
