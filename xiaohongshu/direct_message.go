package xiaohongshu

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/xpzouying/xiaohongshu-mcp/humanize"
)

const (
	directMessageDraftTTL = 10 * time.Minute
	directMessageInterval = time.Minute
)

type DirectMessageDraft struct {
	ConfirmationToken string    `json:"confirmation_token"`
	ExpiresAt         time.Time `json:"expires_at"`
	Recipient         UserSearchItem `json:"recipient"`
	Content           string    `json:"content"`
}

type DirectMessageResult struct {
	Recipient UserSearchItem `json:"recipient"`
	Content   string         `json:"content"`
	Sent      bool           `json:"sent"`
	Message   string         `json:"message"`
}

type directMessageDraftRecord struct {
	DirectMessageDraft
	used bool
}

var directMessageState = struct {
	sync.Mutex
	drafts   map[string]*directMessageDraftRecord
	lastSend time.Time
}{drafts: make(map[string]*directMessageDraftRecord)}

// PrepareDirectMessage resolves one exact public Red ID and verifies that its
// profile exposes a private-message entry. It does not type or send content.
func PrepareDirectMessage(ctx context.Context, page *rod.Page, redID, content string) (*DirectMessageDraft, error) {
	redID = strings.TrimSpace(redID)
	content = strings.TrimSpace(content)
	if redID == "" {
		return nil, fmt.Errorf("小红书号不能为空")
	}
	if content == "" {
		return nil, fmt.Errorf("私信正文不能为空")
	}
	if len([]rune(content)) > 1000 {
		return nil, fmt.Errorf("私信正文过长（最多 1000 个字符）")
	}

	search := NewUserSearchAction(page)
	items, err := search.Search(ctx, redID)
	if err != nil {
		return nil, err
	}
	matches := exactRedIDMatches(items, redID)
	if len(matches) != 1 {
		return nil, fmt.Errorf("小红书号 %s 精确匹配到 %d 个用户，已中止", redID, len(matches))
	}
	recipient := matches[0]

	if err := openRecipientChat(ctx, page, recipient, true); err != nil {
		return nil, err
	}

	token, err := randomConfirmationToken()
	if err != nil {
		return nil, fmt.Errorf("生成确认码失败: %w", err)
	}
	draft := DirectMessageDraft{
		ConfirmationToken: token,
		ExpiresAt:         time.Now().Add(directMessageDraftTTL),
		Recipient:         recipient,
		Content:           content,
	}
	directMessageState.Lock()
	pruneExpiredDraftsLocked(time.Now())
	directMessageState.drafts[token] = &directMessageDraftRecord{DirectMessageDraft: draft}
	directMessageState.Unlock()
	return &draft, nil
}

// SendPreparedDirectMessage consumes a prepared one-time confirmation token.
// The token is burned immediately before clicking Send so an uncertain result
// can never be retried automatically and create a duplicate.
func SendPreparedDirectMessage(ctx context.Context, page *rod.Page, token string) (*DirectMessageResult, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("确认码不能为空")
	}

	directMessageState.Lock()
	pruneExpiredDraftsLocked(time.Now())
	record, ok := directMessageState.drafts[token]
	if !ok || record.used {
		directMessageState.Unlock()
		return nil, fmt.Errorf("确认码无效、已使用或已过期")
	}
	if wait := directMessageInterval - time.Since(directMessageState.lastSend); !directMessageState.lastSend.IsZero() && wait > 0 {
		directMessageState.Unlock()
		return nil, fmt.Errorf("私信限流：请等待 %s 后再发送", wait.Round(time.Second))
	}
	draft := record.DirectMessageDraft
	directMessageState.Unlock()

	if err := openRecipientChat(ctx, page, draft.Recipient, true); err != nil {
		return nil, err
	}
	editor, err := findMessageEditor(page)
	if err != nil {
		return nil, err
	}
	before := countDeliveredText(page, draft.Content, editor)
	if err := humanize.Type(ctx, editor, draft.Content); err != nil {
		return nil, fmt.Errorf("无法输入私信正文: %w", err)
	}
	humanize.Delay(ctx, humanize.AfterType)

	sendButton, err := findVisibleExactText(page, []string{"发送"}, "button, [role=button]")
	if err != nil {
		return nil, fmt.Errorf("未找到明确的发送按钮，未发送: %w", err)
	}

	// Burn the token just before the irreversible click.
	directMessageState.Lock()
	current, stillValid := directMessageState.drafts[token]
	if !stillValid || current.used {
		directMessageState.Unlock()
		return nil, fmt.Errorf("确认码已被使用，已中止")
	}
	current.used = true
	directMessageState.lastSend = time.Now()
	directMessageState.Unlock()

	humanize.Delay(ctx, humanize.BeforeSubmit)
	if err := humanize.Click(sendButton); err != nil {
		return nil, fmt.Errorf("点击发送时失败；确认码已作废以防重复发送: %w", err)
	}

	if !waitMessageDelivered(page, draft.Content, editor, before, 12*time.Second) {
		return nil, fmt.Errorf("已点击发送，但未能确认正文进入消息区；结果不确定，请人工查看，勿自动重试")
	}
	return &DirectMessageResult{
		Recipient: draft.Recipient,
		Content:   draft.Content,
		Sent:      true,
		Message:   "私信已发送并在消息区确认出现",
	}, nil
}

func exactRedIDMatches(items []UserSearchItem, redID string) []UserSearchItem {
	out := make([]UserSearchItem, 0, 1)
	for _, item := range items {
		if item.RedID == redID {
			out = append(out, item)
		}
	}
	return out
}

func randomConfirmationToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func pruneExpiredDraftsLocked(now time.Time) {
	for token, draft := range directMessageState.drafts {
		if draft.used || now.After(draft.ExpiresAt) {
			delete(directMessageState.drafts, token)
		}
	}
}

func openRecipientChat(ctx context.Context, page *rod.Page, recipient UserSearchItem, requireEditor bool) error {
	pp := page.Context(ctx).Timeout(75 * time.Second)
	pp.MustNavigate(recipient.ProfileURL).MustWaitLoad()
	humanize.Delay(ctx, humanize.AfterNavigate)

	// Verify the public identity again on the destination page before opening
	// chat. Both identifiers are required when available.
	deadline := time.Now().Add(15 * time.Second)
	verified := false
	for time.Now().Before(deadline) {
		body, err := pp.Element("body")
		if err == nil {
			text, _ := body.Text()
			verified = strings.Contains(text, recipient.RedID)
			if recipient.Nickname != "" {
				verified = verified && strings.Contains(text, recipient.Nickname)
			}
			if verified {
				break
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !verified {
		return fmt.Errorf("主页身份复核失败，未打开私信")
	}

	entry, err := findVisibleExactText(pp, []string{"私信", "发消息"}, "button, [role=button], a")
	if err != nil {
		return fmt.Errorf("该主页没有可确认的私信入口: %w", err)
	}
	humanize.Delay(ctx, humanize.BeforeClick)
	if err := humanize.Click(entry); err != nil {
		return fmt.Errorf("无法打开私信: %w", err)
	}

	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		info, infoErr := pp.Info()
		if infoErr == nil && strings.Contains(info.URL, "/chat") {
			if !requireEditor {
				return nil
			}
			if _, editorErr := findMessageEditor(pp); editorErr == nil {
				return nil
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("点击私信后没有进入可编辑的聊天页，未发送")
}

func findVisibleExactText(page *rod.Page, texts []string, selector string) (*rod.Element, error) {
	elems, err := page.Elements(selector)
	if err != nil {
		return nil, err
	}
	for _, elem := range elems {
		visible, _ := elem.Visible()
		if !visible {
			continue
		}
		text, textErr := elem.Text()
		if textErr != nil {
			continue
		}
		for _, want := range texts {
			if strings.TrimSpace(text) == want {
				return elem, nil
			}
		}
	}
	return nil, fmt.Errorf("找不到文本为 %s 的可见控件", strings.Join(texts, "/"))
}

func findMessageEditor(page *rod.Page) (*rod.Element, error) {
	selectors := []string{"textarea", "[contenteditable=true]", "[role=textbox]"}
	for _, selector := range selectors {
		elems, _ := page.Elements(selector)
		for _, elem := range elems {
			visible, _ := elem.Visible()
			if visible {
				return elem, nil
			}
		}
	}
	return nil, fmt.Errorf("聊天页没有出现可见的消息编辑框")
}

func countDeliveredText(page *rod.Page, content string, editor *rod.Element) int {
	res, err := page.Eval(`(text) => [...document.querySelectorAll('*')].filter(el => {
		if (!el.children.length && (el.textContent || '').trim() === text) {
			const r = el.getBoundingClientRect();
			const s = getComputedStyle(el);
			return r.width > 0 && r.height > 0 && s.display !== 'none' && s.visibility !== 'hidden';
		}
		return false;
	}).length`, content)
	if err != nil {
		return 0
	}
	return res.Value.Int()
}

func waitMessageDelivered(page *rod.Page, content string, editor *rod.Element, before int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		count := countDeliveredText(page, content, editor)
		value, _ := editor.Property("value")
		editorEmpty := strings.TrimSpace(value.Str()) == ""
		if count > before && editorEmpty {
			return true
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false
}
