package xiaohongshu

import "testing"

func TestParseUserCardLines(t *testing.T) {
	nick, redID := parseUserCardLines([]string{"南瓜妹妹", "小红书号：95412119463", "12篇笔记"})
	if nick != "南瓜妹妹" || redID != "95412119463" {
		t.Fatalf("got nickname=%q redID=%q", nick, redID)
	}
}

func TestCompactLines(t *testing.T) {
	got := compactLines("  A \n\n 小红书号: 123 \r\n")
	if len(got) != 2 || got[0] != "A" || got[1] != "小红书号: 123" {
		t.Fatalf("unexpected lines: %#v", got)
	}
}
