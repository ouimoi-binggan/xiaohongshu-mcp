package xiaohongshu

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/xpzouying/xiaohongshu-mcp/humanize"
)

// FavoriteAlbumItem 是收藏专辑中的一条笔记。
type FavoriteAlbumItem struct {
	FeedID    string `json:"feed_id"`
	Title     string `json:"title"`
	XsecToken string `json:"xsec_token,omitempty"`
	Index     int    `json:"index"`
}

// FavoriteAlbum 是收藏专辑及其笔记。
type FavoriteAlbum struct {
	ID        string              `json:"id"`
	Name      string              `json:"name"`
	NoteCount int                 `json:"note_count"`
	Private   bool                `json:"private"`
	Notes     []FavoriteAlbumItem `json:"notes"`
}

// FavoriteAlbumResult 是收藏到指定专辑的结果。
type FavoriteAlbumResult struct {
	FeedID   string `json:"feed_id"`
	Album    string `json:"album"`
	Success  bool   `json:"success"`
	Message  string `json:"message"`
}

type albumSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	NoteCount int    `json:"note_count"`
	Private   bool   `json:"private"`
}

// FavoriteAlbumAction 处理收藏专辑的读取与写入。
type FavoriteAlbumAction struct {
	page *rod.Page
}

func NewFavoriteAlbumAction(page *rod.Page) *FavoriteAlbumAction {
	return &FavoriteAlbumAction{page: page}
}

// GetByName 按完整名称读取当前账号的收藏专辑。
func (a *FavoriteAlbumAction) GetByName(ctx context.Context, name string) (*FavoriteAlbum, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("收藏夹名称不能为空")
	}
	page := a.page.Context(ctx).Timeout(90 * time.Second)
	if err := NewNavigate(page).ToProfilePage(ctx); err != nil {
		return nil, fmt.Errorf("打开个人主页失败: %w", err)
	}
	page.MustWaitStable()
	profile := NewUserProfileAction(page)
	if err := profile.selectTab(ctx, page, TabFavorites); err != nil {
		return nil, err
	}
	if err := clickExactText(ctx, page, `.reds-tab-item`, "专辑"); err != nil {
		return nil, fmt.Errorf("打开收藏专辑列表失败: %w", err)
	}
	page.MustWaitStable()

	albums, err := readAlbumSummaries(page)
	if err != nil {
		return nil, err
	}
	matches := make([]albumSummary, 0, 1)
	for _, album := range albums {
		if album.Name == name {
			matches = append(matches, album)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("未找到收藏夹 %q", name)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("找到多个同名收藏夹 %q，请先改成不同名称", name)
	}

	album := matches[0]
	page.MustNavigate("https://www.xiaohongshu.com/board/" + album.ID).MustWaitLoad()
	page.MustWaitStable()
	notes, err := readAlbumNotes(page, album.NoteCount)
	if err != nil {
		return nil, err
	}
	return &FavoriteAlbum{ID: album.ID, Name: album.Name, NoteCount: album.NoteCount, Private: album.Private, Notes: notes}, nil
}

// FavoriteToAlbum 收藏笔记后，把它加入指定专辑。
func (a *FavoriteAlbumAction) FavoriteToAlbum(ctx context.Context, feedID, xsecToken, albumName string) error {
	albumName = strings.TrimSpace(albumName)
	if albumName == "" {
		return fmt.Errorf("收藏夹名称不能为空")
	}
	// 先确认目标专辑存在；若笔记已经在其中，直接视为成功。
	album, err := a.GetByName(ctx, albumName)
	if err != nil {
		return err
	}
	for _, note := range album.Notes {
		if note.FeedID == feedID {
			return nil
		}
	}
	page := newInteractAction(a.page).preparePage(ctx, actionFavorite, feedID, xsecToken)
	_, collected, err := newInteractAction(page).getInteractState(page, feedID)
	if err != nil {
		return fmt.Errorf("无法确认当前收藏状态: %w", err)
	}
	if collected {
		return fmt.Errorf("笔记已经收藏；为避免误取消收藏，请先确认它是否已在收藏夹 %q 中", albumName)
	}

	collect, err := page.Element(`#note-page-collect-board-guide`)
	if err != nil {
		return fmt.Errorf("未找到收藏按钮: %w", err)
	}
	if err := humanize.Click(collect); err != nil {
		return fmt.Errorf("收藏笔记失败: %w", err)
	}
	join, err := waitExactText(page, `.tooltip-container .right-area, .right-area`, "加入专辑", 6*time.Second)
	if err != nil {
		return fmt.Errorf("笔记已收藏，但没有出现“加入专辑”入口: %w", err)
	}
	if err := humanize.Click(join); err != nil {
		return fmt.Errorf("打开专辑列表失败: %w", err)
	}
	board, err := waitBoardByName(page, albumName, 8*time.Second)
	if err != nil {
		return fmt.Errorf("未找到收藏夹 %q: %w", albumName, err)
	}
	if err := board.ScrollIntoView(); err != nil {
		return fmt.Errorf("滚动到收藏夹 %q 失败: %w", albumName, err)
	}
	if err := humanize.Click(board); err != nil {
		return fmt.Errorf("选择收藏夹 %q 失败: %w", albumName, err)
	}
	if _, err := waitExactText(page, `.message-container, .msg-container, .left-area`, "已加入"+albumName, 8*time.Second); err != nil {
		return fmt.Errorf("已选择收藏夹，但没有收到明确的加入成功提示: %w", err)
	}
	return nil
}

func clickExactText(ctx context.Context, page *rod.Page, selector, prefix string) error {
	elems, err := page.Elements(selector)
	if err != nil {
		return err
	}
	var matches []*rod.Element
	for _, elem := range elems {
		text, textErr := elem.Text()
		if textErr == nil && (strings.TrimSpace(text) == prefix || strings.HasPrefix(strings.TrimSpace(text), prefix+"・")) {
			matches = append(matches, elem)
		}
	}
	if len(matches) != 1 {
		return fmt.Errorf("文字为 %q 的可操作项数量为 %d", prefix, len(matches))
	}
	humanize.Delay(ctx, humanize.BeforeClick)
	return humanize.Click(matches[0])
}

func waitExactText(page *rod.Page, selector, text string, timeout time.Duration) (*rod.Element, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		elems, _ := page.Elements(selector)
		var matches []*rod.Element
		for _, elem := range elems {
			value, err := elem.Text()
			if err == nil && strings.TrimSpace(value) == text {
				visible, _ := elem.Visible()
				if visible {
					matches = append(matches, elem)
				}
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			return nil, fmt.Errorf("找到多个同名项")
		}
		time.Sleep(150 * time.Millisecond)
	}
	return nil, fmt.Errorf("等待 %q 超时", text)
}

func readAlbumSummaries(page *rod.Page) ([]albumSummary, error) {
	seen := map[string]albumSummary{}
	stable := 0
	for attempt := 0; attempt < 50; attempt++ {
		raw := page.MustEval(`() => JSON.stringify(Array.from(document.querySelectorAll('a[href*="/board/"]')).map(a => {
		const href = a.getAttribute('href') || '';
		const match = href.match(/\/board\/([a-f0-9]{24})/i);
		const name = (a.querySelector('.title')?.innerText || '').trim();
		const text = (a.innerText || '').trim();
		const count = text.match(/笔记[・·]\s*(\d+)/);
		return match && name ? {id: match[1], name, note_count: count ? Number(count[1]) : 0,
			private: text.includes('私密') || !!a.querySelector('[class*="lock"]')} : null;
	}).filter(Boolean))`).String()
		var batch []albumSummary
		if err := json.Unmarshal([]byte(raw), &batch); err != nil {
			return nil, fmt.Errorf("解析收藏专辑失败: %w", err)
		}
		before := len(seen)
		for _, album := range batch {
			seen[album.ID] = album
		}
		if len(seen) == before {
			stable++
		} else {
			stable = 0
		}
		if stable >= 5 {
			break
		}
		page.MustEval(`() => window.scrollBy(0, Math.max(400, window.innerHeight * 0.8))`)
		time.Sleep(300 * time.Millisecond)
	}
	albums := make([]albumSummary, 0, len(seen))
	for _, album := range seen {
		albums = append(albums, album)
	}
	return albums, nil
}

func waitBoardByName(page *rod.Page, name string, timeout time.Duration) (*rod.Element, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if elem, err := waitExactText(page, `.board-list .board-item`, name, 300*time.Millisecond); err == nil {
			return elem, nil
		}
		page.MustEval(`() => {
			const root = document.querySelector('.board-list-container');
			if (!root) return;
			const panel = [root, ...root.querySelectorAll('*')].find(e => {
				const s = getComputedStyle(e);
				return /^(auto|scroll)$/.test(s.overflowY) && e.scrollHeight > e.clientHeight;
			}) || root;
			panel.scrollTop += Math.max(100, panel.clientHeight - 24);
		}`)
		time.Sleep(120 * time.Millisecond)
	}
	return nil, fmt.Errorf("等待 %q 超时", name)
}

func readAlbumNotes(page *rod.Page, expected int) ([]FavoriteAlbumItem, error) {
	items := map[string]FavoriteAlbumItem{}
	stable := 0
	for attempt := 0; attempt < 80; attempt++ {
		raw := page.MustEval(`() => JSON.stringify(Array.from(document.querySelectorAll('section.note-item')).map((e, i) => {
			const a = e.querySelector('a[href*="/explore/"],a[href*="/discovery/item/"]');
			const href = a?.getAttribute('href') || '';
			const id = e.dataset.noteId || (href.match(/(?:explore|discovery\/item)\/([a-f0-9]{24})/i) || [])[1] || '';
			return {feed_id:id, title:(e.querySelector('a.title')?.innerText || '').trim(), href,
				index:Number(e.dataset.index ?? i)};
		}).filter(x => x.feed_id))`).String()
		var batch []struct {
			FeedID string `json:"feed_id"`
			Title  string `json:"title"`
			Href   string `json:"href"`
			Index  int    `json:"index"`
		}
		if err := json.Unmarshal([]byte(raw), &batch); err != nil {
			return nil, fmt.Errorf("解析收藏夹笔记失败: %w", err)
		}
		before := len(items)
		for _, item := range batch {
			token := ""
			if parsed, err := url.Parse(item.Href); err == nil {
				token = parsed.Query().Get("xsec_token")
			}
			items[item.FeedID] = FavoriteAlbumItem{FeedID: item.FeedID, Title: item.Title, XsecToken: token, Index: item.Index}
		}
		if expected > 0 && len(items) >= expected {
			break
		}
		if len(items) == before {
			stable++
		} else {
			stable = 0
		}
		if stable >= 6 {
			break
		}
		page.MustEval(`() => window.scrollBy(0, Math.max(400, window.innerHeight * 0.8))`)
		time.Sleep(350 * time.Millisecond)
	}
	notes := make([]FavoriteAlbumItem, 0, len(items))
	for _, item := range items {
		notes = append(notes, item)
	}
	sort.Slice(notes, func(i, j int) bool { return notes[i].Index < notes[j].Index })
	return notes, nil
}
