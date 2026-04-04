package cmd

import (
	"bytes"
	"strings"
	"testing"

	wx "github.com/daemon365/weixin-clawbot"
)

func TestWeixinListShowsSavedAccounts(t *testing.T) {
	dir := t.TempDir()
	if _, err := wx.SaveAccount(dir, &wx.Account{
		AccountID: "a@im.wechat",
		BotToken:  "token-a",
		UserID:    "user-a",
		SavedAt:   "2026-03-23T00:00:00Z",
		BaseURL:   "https://example-a",
	}); err != nil {
		t.Fatalf("SaveAccount a: %v", err)
	}
	if _, err := wx.SaveAccount(dir, &wx.Account{
		AccountID: "b@im.wechat",
		BotToken:  "token-b",
	}); err != nil {
		t.Fatalf("SaveAccount b: %v", err)
	}

	cmd := newWeixinListCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--save-dir", dir})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"weixin accounts in " + dir,
		"a@im.wechat",
		"user=user-a",
		"saved_at=2026-03-23T00:00:00Z",
		"base_url=https://example-a",
		"b@im.wechat",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output = %q, want substring %q", got, want)
		}
	}
}

func TestWeixinListHandlesEmptyDirectory(t *testing.T) {
	cmd := newWeixinListCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--save-dir", t.TempDir()})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "no weixin accounts found") {
		t.Fatalf("output = %q", out.String())
	}
}
