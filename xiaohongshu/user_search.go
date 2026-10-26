package xiaohongshu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/xpzouying/xiaohongshu-mcp/humanize"
)

var profileIDPattern = regexp.MustCompile(`/user/profile/([^/?#]+)`)

// UserSearchItem is the public identity information needed to verify a
// recipient before any direct-message action.
type UserSearchItem struct {
	UserID     string `json:"user_id"`
	Nickname   string `json:"nickname,omitempty"`
	RedID      string `json:"red_id,omitempty"`
	ProfileURL string `json:"profile_url"`
	XsecToken  string `json:"xsec_token,omitempty"`
}

type UserSearchAction struct {
	page *rod.Page
}

func NewUserSearchAction(page *rod.Page) *UserSearchAction {
	return &UserSearchAction{page: page}
}

func (s *UserSearchAction) Search(ctx context.Context, keyword string) ([]UserSearchItem, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, fmt.Errorf("搜索关键词不能为空")
	}

	page := s.page.Context(ctx).Timeout(75 * time.Second)
	page.MustNavigate(makeUserSearchURL(keyword)).MustWaitLoad()
	humanize.Delay(ctx, humanize.AfterNavigate)

	// The search page defaults to notes. Click the visible exact-text 用户 tab
	// without relying on a rollout-specific class name.
	clicked, err := page.Eval(`() => {
		const visible = el => {
			const r = el.getBoundingClientRect();
			const s = getComputedStyle(el);
			return r.width > 0 && r.height > 0 && s.display !== 'none' && s.visibility !== 'hidden';
		};
		const candidates = [...document.querySelectorAll('[role="tab"], .channel, .reds-tab-item, button, span')]
			.filter(x => visible(x) && (x.textContent || '').trim() === '用户')
			.sort((a, b) => a.querySelectorAll('*').length - b.querySelectorAll('*').length);
		if (!candidates.length) return false;
		const leaf = candidates[0];
		const el = leaf.closest('[role="tab"], .channel, .reds-tab-item, button') || leaf;
		el.click();
		return true;
	}`)
	if err != nil {
		return nil, fmt.Errorf("切换到用户搜索失败: %w", err)
	}
	if !clicked.Value.Bool() {
		return nil, fmt.Errorf("搜索页没有找到「用户」分区，页面结构可能已变化")
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		count, evalErr := page.Eval(`() => document.querySelectorAll('a[href*="/user/profile/"]').length`)
		if evalErr == nil && count.Value.Int() > 1 {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	humanize.Delay(ctx, humanize.Reading)

	res, err := page.Eval(`() => {
		const visible = el => {
			const r = el.getBoundingClientRect();
			const s = getComputedStyle(el);
			return r.width > 0 && r.height > 0 && s.display !== 'none' && s.visibility !== 'hidden';
		};
		const links = [...document.querySelectorAll('a[href*="/user/profile/"]')].filter(a =>
			visible(a) && !a.closest('nav, .side-bar, .side-bar-container, .side-bar-component')
		);
		return JSON.stringify(links.map(a => {
			const u = new URL(a.href, location.href);
			let card = a;
			for (let i = 0; i < 5 && card.parentElement; i++, card = card.parentElement) {
				const text = (card.innerText || '').trim();
				if (/小红书号[：:]?\s*\S+/.test(text)) break;
			}
			const text = (card.innerText || a.innerText || '').trim().slice(0, 800);
			return {href: u.href, path: u.pathname, query: u.search, text};
		}));
	}`)
	if err != nil {
		return nil, fmt.Errorf("读取用户搜索结果失败: %w", err)
	}

	var cards []struct {
		Href  string `json:"href"`
		Path  string `json:"path"`
		Query string `json:"query"`
		Text  string `json:"text"`
	}
	if err := json.Unmarshal([]byte(res.Value.Str()), &cards); err != nil {
		return nil, fmt.Errorf("解析用户搜索结果失败: %w", err)
	}

	byID := make(map[string]UserSearchItem)
	for _, card := range cards {
		m := profileIDPattern.FindStringSubmatch(card.Path)
		if len(m) != 2 {
			continue
		}
		userID := m[1]
		// Exclude the persistent sidebar link to the signed-in user's own page:
		// real result cards carry text, while the sidebar anchor does not.
		lines := compactLines(card.Text)
		if len(lines) == 0 {
			continue
		}
		item := UserSearchItem{UserID: userID, ProfileURL: card.Href}
		item.Nickname, item.RedID = parseUserCardLines(lines)
		if item.RedID == "" {
			continue
		}
		if parsed, parseErr := url.Parse(card.Href); parseErr == nil {
			item.XsecToken = parsed.Query().Get("xsec_token")
		}
		byID[userID] = item
	}

	items := make([]UserSearchItem, 0, len(byID))
	for _, item := range byID {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].RedID == items[j].RedID {
			return items[i].Nickname < items[j].Nickname
		}
		return items[i].RedID < items[j].RedID
	})
	return items, nil
}

func makeUserSearchURL(keyword string) string {
	values := url.Values{}
	values.Set("keyword", keyword)
	values.Set("source", "web_search_result_users")
	values.Set("target_search", "users")
	values.Set("type", "user")
	return fmt.Sprintf("https://www.xiaohongshu.com/search_result?%s", values.Encode())
}

func compactLines(text string) []string {
	parts := strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseUserCardLines(lines []string) (nickname, redID string) {
	for _, line := range lines {
		if strings.Contains(line, "小红书号") {
			parts := strings.FieldsFunc(line, func(r rune) bool {
				return r == '：' || r == ':'
			})
			if len(parts) > 1 {
				redID = strings.TrimSpace(parts[len(parts)-1])
			}
			continue
		}
		if nickname == "" {
			nickname = line
		}
	}
	return nickname, redID
}
