package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/yesvichu/sub2surge/internal/convert"
	"github.com/yesvichu/sub2surge/internal/surgefile"
	"github.com/yesvichu/sub2surge/internal/version"
)

// Converter is the Go service the frontend calls. Every exported method is
// available to the page as Converter.<method>.
type Converter struct {
	service *convert.Service
	history *historyStore

	// Surge is detected on first use, so startup runs no subprocess.
	surgeMu      sync.Mutex
	surgeInstall *surgeSetup
}

// NewConverter creates the service with a one-hour upstream cache.
func NewConverter() *Converter {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		// PathUserData is only a convenience; keep the app usable if the
		// platform cannot resolve its user data directory.
		log.Printf("history: user data directory: %v", err)
		dir = os.TempDir()
	}
	return newConverter(dir)
}

// newConverter builds the service with its history stored in dir. Tests use
// it to keep their history out of the real user data directory.
func newConverter(dir string) *Converter {
	return &Converter{
		service: convert.New(time.Hour, "v2rayN/6.31"),
		history: newHistoryStore(filepath.Join(dir, historyFilename)),
	}
}

// ConvertRequest is the form state sent by the page.
type ConvertRequest struct {
	URL      string `json:"url"`
	URL2     string `json:"url2"`
	Label1   string `json:"label1"`
	Label2   string `json:"label2"`
	Merge    bool   `json:"merge"`
	UA       string `json:"ua"`
	Prefix   string `json:"prefix"`
	Filter   string `json:"filter"`
	Exclude  string `json:"exclude"`
	Sort     bool   `json:"sort"`
	Insecure bool   `json:"insecure"`
	UDP      bool   `json:"udp"`
	TFO      bool   `json:"tfo"`
	Rules    bool   `json:"rules"`
	Groups   string `json:"groups"`
}

// SkipView is one unsupported or failed node, safe to show in the UI.
type SkipView struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

// ConvertResponse contains both output formats so the UI can switch instantly.
type ConvertResponse struct {
	Config    string     `json:"config"`
	List      string     `json:"list"`
	Total     int        `json:"total"`
	Converted int        `json:"converted"`
	Skipped   []SkipView `json:"skipped"`
	Warnings  []string   `json:"warnings"`
	ElapsedMS int64      `json:"elapsed_ms"`
	Sources   int        `json:"sources"`
}

// Convert fetches the subscription and returns a Surge profile plus a plain
// proxy list. Errors are returned as messages the page can show.
func (c *Converter) Convert(ctx context.Context, req ConvertRequest) (ConvertResponse, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	selection := surgefile.ParseSelection(req.Groups)

	var (
		result *convert.Result
		err    error
	)
	if req.Merge {
		result, err = c.service.RunMerge(ctx, convert.MergeRequest{
			Sources: []convert.Source{
				{URL: req.URL, Label: req.Label1, UA: req.UA},
				{URL: req.URL2, Label: req.Label2, UA: req.UA},
			},
			Prefix:   req.Prefix,
			Filter:   req.Filter,
			Exclude:  req.Exclude,
			Sort:     req.Sort,
			Insecure: req.Insecure,
			UDP:      req.UDP,
			TFO:      req.TFO,
		})
	} else {
		result, err = c.service.Run(ctx, convert.Request{
			URL:      req.URL,
			UA:       req.UA,
			Prefix:   req.Prefix,
			Filter:   req.Filter,
			Exclude:  req.Exclude,
			Sort:     req.Sort,
			Insecure: req.Insecure,
			UDP:      req.UDP,
			TFO:      req.TFO,
		})
	}
	if err != nil {
		return ConvertResponse{}, errors.New(convert.FormatError(err))
	}

	if len(result.Lines) == 0 {
		return ConvertResponse{}, fmt.Errorf("没有可转换的节点：%s", skipSummary(result))
	}

	parsedSkips := 0
	skips := make([]SkipView, 0, len(result.Skipped))
	for _, skip := range result.Skipped {
		if skip.Line > 0 {
			parsedSkips++
		}
		skips = append(skips, SkipView{
			Name:   skip.Name,
			Type:   skip.Scheme,
			Reason: skip.Reason,
		})
	}

	// A successful pull goes into history so it can be reused or refreshed
	// without typing the subscription URL again. A failed history write must
	// not throw away an otherwise successful conversion.
	if _, err := c.history.record(req, len(result.Lines)); err != nil {
		log.Printf("history: %v", err)
	}

	return ConvertResponse{
		Config: surgefile.RenderProfile(result.Lines, surgefile.ProfileOptions{
			Title:       "Sub2Surge export",
			Rules:       surgefile.RulesFor(req.Rules, selection),
			ExtraGroups: extraGroups(req.Rules, result.Names, selection),
		}),
		List:      surgefile.ProxyLines(result.Lines),
		Total:     len(result.Proxies) + parsedSkips,
		Converted: len(result.Lines),
		Skipped:   skips,
		Warnings:  result.Warnings,
		ElapsedMS: time.Since(start).Milliseconds(),
		Sources:   result.Sources,
	}, nil
}

// Save opens a native save dialog and writes content to the chosen path. It
// returns "" when the user cancels.
func (c *Converter) Save(ctx context.Context, filename, content string) (string, error) {
	if strings.TrimSpace(content) == "" {
		return "", errors.New("没有可保存的内容，请先转换")
	}
	if strings.TrimSpace(filename) == "" {
		filename = "Surge.conf"
	}

	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      mygo.CallerWindow(ctx),
		Title:       "保存为",
		DefaultPath: filename,
		Filters: []mygo.FileFilter{
			{Name: "Surge 配置", Extensions: []string{"conf"}},
			{Name: "文本文件", Extensions: []string{"txt"}},
			{Name: "所有文件", Extensions: []string{"*"}},
		},
		CreateDirectories: true,
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("写入失败：%w", err)
	}
	return path, nil
}

// History lists previously used subscriptions, most recent first.
func (c *Converter) History() []HistoryEntry {
	return c.history.list()
}

// ForgetHistory removes one entry from the history.
func (c *Converter) ForgetHistory(id string) ([]HistoryEntry, error) {
	return c.history.remove(id)
}

// ClearHistory removes every entry from the history.
func (c *Converter) ClearHistory() ([]HistoryEntry, error) {
	return c.history.clear()
}

// Alert shows a native dialog for a failure the user must not miss, such as an
// upstream that refuses the subscription. The page passes the short message and
// a longer hint; the detail is where the actionable advice goes.
func (c *Converter) Alert(ctx context.Context, message, detail string) error {
	if strings.TrimSpace(message) == "" {
		return nil
	}
	_, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:  mygo.CallerWindow(ctx),
		Type:    mygo.MessageError,
		Message: message,
		Detail:  detail,
		Buttons: []string{"知道了"},
	})
	return err
}

// Version is shown in the footer.
func (c *Converter) Version() string {
	return version.Version
}

func extraGroups(include bool, names []string, selection surgefile.Selection) []string {
	if !include {
		return nil
	}
	return surgefile.DefaultProxyGroupsFor(names, selection)
}

// RuleGroups lists the policy groups the page can toggle.
func (c *Converter) RuleGroups() []surgefile.RuleGroup {
	return surgefile.AvailableRuleGroups()
}

func skipSummary(result *convert.Result) string {
	lines := surgefile.SummarizeSkips(result.Skipped, 3)
	if len(lines) == 0 {
		return "订阅里没有节点"
	}
	return strings.Join(lines, "；")
}
