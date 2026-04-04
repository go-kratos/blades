package weixin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	wx "github.com/daemon365/weixin-clawbot"

	"github.com/go-kratos/blades/cmd/blades/internal/config"
)

// NewFromConfig builds a Weixin channel using config values with environment
// fallbacks for credentials and saved-account loading.
func NewFromConfig(cfg config.WeixinConfig, clearSession func(string) error, extraOpts ...Option) (*Channel, error) {
	return newFromConfig(cfg, os.Getenv, wx.LoadAccount, ListSavedAccounts, clearSession, extraOpts...)
}

func newFromConfig(
	cfg config.WeixinConfig,
	getenv func(string) string,
	loadAccount func(string, string) (*wx.Account, error),
	listAccounts func(string) ([]wx.Account, error),
	clearSession func(string) error,
	extraOpts ...Option,
) (*Channel, error) {
	envAccountDir := strings.TrimSpace(getenv("WEIXIN_ACCOUNT_DIR"))
	explicitAccountDir := strings.TrimSpace(cfg.AccountDir) != "" || envAccountDir != ""
	accountDir := strings.TrimSpace(firstNonEmpty(
		cfg.AccountDir,
		envAccountDir,
		DefaultAccountDir(),
	))
	accountDir = config.ExpandTilde(accountDir)
	accountID := firstNonEmpty(cfg.AccountID, getenv("WEIXIN_ACCOUNT_ID"))
	botToken := firstNonEmpty(cfg.BotToken, getenv("WEIXIN_BOT_TOKEN"))
	baseURL := firstNonEmpty(cfg.BaseURL, getenv("WEIXIN_BASE_URL"), wx.DefaultBaseURL)

	account := wx.Account{
		AccountID: strings.TrimSpace(accountID),
		BotToken:  strings.TrimSpace(botToken),
		BaseURL:   strings.TrimSpace(baseURL),
	}
	if account.AccountID == "" && accountDir != "" {
		accounts, err := listAccounts(accountDir)
		if err != nil {
			return nil, fmt.Errorf("list weixin accounts from %s: %w", accountDir, err)
		}
		switch len(accounts) {
		case 0:
		case 1:
			account = accounts[0]
			if strings.TrimSpace(cfg.BaseURL) != "" {
				account.BaseURL = strings.TrimSpace(cfg.BaseURL)
			}
			if strings.TrimSpace(cfg.BotToken) != "" {
				account.BotToken = strings.TrimSpace(cfg.BotToken)
			}
		default:
			return nil, fmt.Errorf("multiple weixin accounts found in %s; set weixin.accountID", accountDir)
		}
	}
	if account.AccountID == "" {
		return nil, fmt.Errorf("no weixin account selected; run `blades weixin login` or set weixin.accountDir / weixin.accountID")
	}
	if account.BotToken == "" && accountDir != "" && (explicitAccountDir || strings.TrimSpace(accountID) == "") {
		loaded, err := loadAccount(accountDir, account.AccountID)
		if err != nil {
			return nil, fmt.Errorf("load weixin account %q from %s: %w", account.AccountID, accountDir, err)
		}
		if loaded != nil {
			account = *loaded
			if strings.TrimSpace(cfg.BaseURL) != "" {
				account.BaseURL = strings.TrimSpace(cfg.BaseURL)
			}
			if strings.TrimSpace(cfg.BotToken) != "" {
				account.BotToken = strings.TrimSpace(cfg.BotToken)
			}
		}
	}
	if strings.TrimSpace(account.BotToken) == "" {
		return nil, fmt.Errorf("weixin account %q is missing bot token; re-run `blades weixin login` or set weixin.botToken", account.AccountID)
	}
	if strings.TrimSpace(account.BaseURL) == "" {
		account.BaseURL = wx.DefaultBaseURL
	}

	stateDir := strings.TrimSpace(firstNonEmpty(cfg.StateDir, getenv("WEIXIN_STATE_DIR"), DefaultRootDir()))
	stateDir = config.ExpandTilde(stateDir)
	mediaDir := strings.TrimSpace(firstNonEmpty(cfg.MediaDir, filepath.Join(stateDir, "media")))
	mediaDir = config.ExpandTilde(mediaDir)
	opts := []Option{
		WithClearSession(clearSession),
	}
	opts = append(opts, extraOpts...)
	ch := New(account, syncBufPath(stateDir, account.AccountID), opts...)
	ch.routeTag = strings.TrimSpace(firstNonEmpty(cfg.RouteTag, getenv("WEIXIN_ROUTE_TAG")))
	ch.channelVersion = strings.TrimSpace(firstNonEmpty(cfg.ChannelVersion, getenv("WEIXIN_CHANNEL_VERSION"), "blades"))
	ch.cdnBaseURL = strings.TrimSpace(firstNonEmpty(cfg.CDNBaseURL, getenv("WEIXIN_CDN_BASE_URL")))
	ch.mediaDir = mediaDir
	ch.allowFrom = append([]string(nil), cfg.AllowFrom...)
	ch.debug = cfg.Debug
	return ch, nil
}

func DefaultRootDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".blades", "weixin")
	}
	return filepath.Join(home, ".blades", "weixin")
}

func DefaultAccountDir() string {
	return filepath.Join(DefaultRootDir(), "account")
}

func DefaultSyncDir() string {
	return filepath.Join(DefaultRootDir(), "sync")
}

func DefaultMediaDir() string {
	return filepath.Join(DefaultRootDir(), "media")
}

func syncBufPath(stateDir, accountID string) string {
	return filepath.Join(stateDir, "sync", accountID+".sync.json")
}

func ListSavedAccounts(dir string) ([]wx.Account, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	accounts := make([]wx.Account, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".sync.json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		var account wx.Account
		if err := json.Unmarshal(data, &account); err != nil {
			return nil, err
		}
		if strings.TrimSpace(account.AccountID) == "" {
			continue
		}
		accounts = append(accounts, account)
	}
	return accounts, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
