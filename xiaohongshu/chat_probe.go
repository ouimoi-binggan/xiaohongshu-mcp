package xiaohongshu

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/xpzouying/xiaohongshu-mcp/humanize"
)

// ChatControl describes one non-content control on the chat page. It
// intentionally excludes innerText/value so probing never returns contacts or
// message bodies.
type ChatControl struct {
	Tag             string `json:"tag"`
	Type            string `json:"type,omitempty"`
	Placeholder     string `json:"placeholder,omitempty"`
	AriaLabel       string `json:"aria_label,omitempty"`
	Role            string `json:"role,omitempty"`
	Title           string `json:"title,omitempty"`
	ContentEditable string `json:"content_editable,omitempty"`
	ClassName       string `json:"class_name,omitempty"`
	HrefPath        string `json:"href_path,omitempty"`
}

// ChatCapabilities is a privacy-preserving structural snapshot used to keep
// chat automation compatible with staged XHS web rollouts.
type ChatCapabilities struct {
	URL              string        `json:"url"`
	Title            string        `json:"title"`
	InitialStateKeys []string      `json:"initial_state_keys"`
	Controls         []ChatControl `json:"controls"`
}

type ChatAction struct {
	page *rod.Page
}

func NewChatAction(page *rod.Page) *ChatAction { return &ChatAction{page: page} }

// Probe opens /chat and returns only page structure. It never reads contact
// names, input values, conversation previews, or message contents.
func (c *ChatAction) Probe(ctx context.Context) (*ChatCapabilities, error) {
	page := c.page.Context(ctx).Timeout(60 * time.Second)
	page.MustNavigate("https://www.xiaohongshu.com/chat").MustWaitLoad()
	humanize.Delay(ctx, humanize.AfterNavigate)

	res, err := page.Eval(`() => {
		const clean = (v, n = 160) => typeof v === "string" ? v.slice(0, n) : "";
		const controls = [...document.querySelectorAll(
			'input, textarea, button, [role="button"], [contenteditable="true"], a[href]'
		)].filter(el => {
			const r = el.getBoundingClientRect();
			const s = getComputedStyle(el);
			return r.width > 0 && r.height > 0 && s.visibility !== 'hidden' && s.display !== 'none';
		}).slice(0, 120).map(el => {
			let hrefPath = "";
			if (el.tagName === "A" && el.href) {
				try { hrefPath = new URL(el.href, location.href).pathname; } catch (_) {}
			}
			return {
				tag: el.tagName.toLowerCase(),
				type: clean(el.getAttribute('type')),
				placeholder: clean(el.getAttribute('placeholder')),
				aria_label: clean(el.getAttribute('aria-label')),
				role: clean(el.getAttribute('role')),
				title: clean(el.getAttribute('title')),
				content_editable: clean(el.getAttribute('contenteditable')),
				class_name: clean(typeof el.className === 'string' ? el.className : ''),
				href_path: clean(hrefPath),
			};
		});
		return JSON.stringify({
			url: location.href,
			title: document.title,
			initial_state_keys: Object.keys(window.__INITIAL_STATE__ || {}).sort(),
			controls,
		});
	}`)
	if err != nil {
		return nil, fmt.Errorf("检查聊天页结构失败: %w", err)
	}

	var out ChatCapabilities
	if err := json.Unmarshal([]byte(res.Value.Str()), &out); err != nil {
		return nil, fmt.Errorf("解析聊天页结构失败: %w", err)
	}
	if out.URL == "" {
		return nil, fmt.Errorf("聊天页没有返回有效地址，可能未登录或页面尚未开放")
	}
	return &out, nil
}
