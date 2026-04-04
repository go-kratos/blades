package weixin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	wx "github.com/daemon365/weixin-clawbot"

	"github.com/go-kratos/blades/cmd/blades/internal/config"
)

func TestNewFromConfigUsesEnvFallbacks(t *testing.T) {
	ch, err := newFromConfig(config.WeixinConfig{}, func(key string) string {
		switch key {
		case "WEIXIN_ACCOUNT_ID":
			return "user@im.wechat"
		case "WEIXIN_BOT_TOKEN":
			return "secret-token"
		default:
			return ""
		}
	}, wx.LoadAccount, ListSavedAccounts, nil)
	if err != nil {
		t.Fatalf("newFromConfig: %v", err)
	}
	if got, want := ch.Name(), "weixin"; got != want {
		t.Fatalf("channel name = %q, want %q", got, want)
	}
	if got, want := ch.account.AccountID, "user@im.wechat"; got != want {
		t.Fatalf("account id = %q, want %q", got, want)
	}
	if got, want := ch.account.BotToken, "secret-token"; got != want {
		t.Fatalf("bot token = %q, want %q", got, want)
	}
}

func TestNewFromConfigLoadsSavedAccount(t *testing.T) {
	dir := t.TempDir()
	if _, err := wx.SaveAccount(dir, &wx.Account{
		AccountID: "bot@im.wechat",
		BotToken:  "saved-token",
		BaseURL:   "https://example.weixin.local",
	}); err != nil {
		t.Fatalf("SaveAccount: %v", err)
	}

	ch, err := newFromConfig(config.WeixinConfig{
		AccountID:  "bot@im.wechat",
		AccountDir: dir,
	}, func(string) string { return "" }, wx.LoadAccount, ListSavedAccounts, nil)
	if err != nil {
		t.Fatalf("newFromConfig load saved account: %v", err)
	}
	if got, want := ch.account.BotToken, "saved-token"; got != want {
		t.Fatalf("bot token = %q, want %q", got, want)
	}
	if got, want := ch.account.BaseURL, "https://example.weixin.local"; got != want {
		t.Fatalf("base url = %q, want %q", got, want)
	}
}

func TestNewFromConfigRejectsMissingCredentials(t *testing.T) {
	oldHome := os.Getenv("HOME")
	home := t.TempDir()
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})

	_, err := newFromConfig(config.WeixinConfig{}, func(string) string { return "" }, wx.LoadAccount, ListSavedAccounts, nil)
	if err == nil || !strings.Contains(err.Error(), "blades weixin login") {
		t.Fatalf("expected missing account guidance, got %v", err)
	}

	_, err = newFromConfig(config.WeixinConfig{
		AccountID: "demo@im.wechat",
	}, func(string) string { return "" }, wx.LoadAccount, ListSavedAccounts, nil)
	if err == nil || !strings.Contains(err.Error(), "missing bot token") {
		t.Fatalf("expected missing token error, got %v", err)
	}
}

func TestNewFromConfigAutoSelectsSingleSavedAccount(t *testing.T) {
	dir := t.TempDir()
	if _, err := wx.SaveAccount(dir, &wx.Account{
		AccountID: "solo@im.wechat",
		BotToken:  "solo-token",
		BaseURL:   "https://solo.example.com",
	}); err != nil {
		t.Fatalf("SaveAccount: %v", err)
	}

	ch, err := newFromConfig(config.WeixinConfig{
		AccountDir: dir,
	}, func(string) string { return "" }, wx.LoadAccount, ListSavedAccounts, nil)
	if err != nil {
		t.Fatalf("newFromConfig auto-select single account: %v", err)
	}
	if got, want := ch.account.AccountID, "solo@im.wechat"; got != want {
		t.Fatalf("account id = %q, want %q", got, want)
	}
}

func TestNewFromConfigRejectsAmbiguousSavedAccounts(t *testing.T) {
	dir := t.TempDir()
	for _, accountID := range []string{"a@im.wechat", "b@im.wechat"} {
		if _, err := wx.SaveAccount(dir, &wx.Account{
			AccountID: accountID,
			BotToken:  "token-" + accountID,
		}); err != nil {
			t.Fatalf("SaveAccount(%s): %v", accountID, err)
		}
	}

	_, err := newFromConfig(config.WeixinConfig{
		AccountDir: dir,
	}, func(string) string { return "" }, wx.LoadAccount, ListSavedAccounts, nil)
	if err == nil || !strings.Contains(err.Error(), "multiple weixin accounts") {
		t.Fatalf("expected ambiguous account error, got %v", err)
	}
}

func TestNewFromConfigExpandsTildeAccountDir(t *testing.T) {
	oldHome := os.Getenv("HOME")
	home := t.TempDir()
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})

	dir := filepath.Join(home, ".blades", "weixin", "account")
	if _, err := wx.SaveAccount(dir, &wx.Account{
		AccountID: "tilde@im.wechat",
		BotToken:  "tilde-token",
	}); err != nil {
		t.Fatalf("SaveAccount: %v", err)
	}

	ch, err := newFromConfig(config.WeixinConfig{
		AccountDir: "~/.blades/weixin/account",
	}, func(string) string { return "" }, wx.LoadAccount, ListSavedAccounts, nil)
	if err != nil {
		t.Fatalf("newFromConfig with tilde path: %v", err)
	}
	if got, want := ch.account.AccountID, "tilde@im.wechat"; got != want {
		t.Fatalf("account id = %q, want %q", got, want)
	}
}

func TestListSavedAccountsIgnoresSyncFiles(t *testing.T) {
	dir := t.TempDir()
	if _, err := wx.SaveAccount(dir, &wx.Account{
		AccountID: "only@im.wechat",
		BotToken:  "token",
	}); err != nil {
		t.Fatalf("SaveAccount: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "only@im.wechat.sync.json"), []byte(`{"get_updates_buf":"x"}`), 0o600); err != nil {
		t.Fatalf("WriteFile sync: %v", err)
	}

	accounts, err := ListSavedAccounts(dir)
	if err != nil {
		t.Fatalf("ListSavedAccounts: %v", err)
	}
	if len(accounts) != 1 || accounts[0].AccountID != "only@im.wechat" {
		t.Fatalf("accounts = %+v", accounts)
	}
}

func TestDefaultDirsAndSyncBufPath(t *testing.T) {
	oldHome := os.Getenv("HOME")
	home := t.TempDir()
	if err := os.Setenv("HOME", home); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})

	if got, want := DefaultRootDir(), filepath.Join(home, ".blades", "weixin"); got != want {
		t.Fatalf("DefaultRootDir = %q, want %q", got, want)
	}
	if got, want := DefaultAccountDir(), filepath.Join(home, ".blades", "weixin", "account"); got != want {
		t.Fatalf("DefaultAccountDir = %q, want %q", got, want)
	}
	if got, want := DefaultSyncDir(), filepath.Join(home, ".blades", "weixin", "sync"); got != want {
		t.Fatalf("DefaultSyncDir = %q, want %q", got, want)
	}
	if got, want := DefaultMediaDir(), filepath.Join(home, ".blades", "weixin", "media"); got != want {
		t.Fatalf("DefaultMediaDir = %q, want %q", got, want)
	}
	if got, want := syncBufPath("/tmp/state", "demo@im.wechat"), "/tmp/state/sync/demo@im.wechat.sync.json"; got != want {
		t.Fatalf("syncBufPath = %q, want %q", got, want)
	}
}
