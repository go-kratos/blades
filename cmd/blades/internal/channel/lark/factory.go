package lark

import (
	"fmt"
	"os"

	"github.com/go-kratos/blades/cmd/blades/internal/config"
)

// NewFromConfig builds a Lark channel using config values with environment
// fallbacks for required credentials.
func NewFromConfig(cfg config.LarkConfig, clearSession func(string) error, extraOpts ...Option) (*Channel, error) {
	return newFromConfig(cfg, os.Getenv, clearSession, extraOpts...)
}

func newFromConfig(cfg config.LarkConfig, getenv func(string) string, clearSession func(string) error, extraOpts ...Option) (*Channel, error) {
	appID := cfg.AppID
	if appID == "" {
		appID = getenv("LARK_APP_ID")
	}
	if appID == "" {
		return nil, fmt.Errorf("lark.appID or LARK_APP_ID is required")
	}

	appSecret := cfg.AppSecret
	if appSecret == "" {
		appSecret = getenv("LARK_APP_SECRET")
	}
	if appSecret == "" {
		return nil, fmt.Errorf("lark.appSecret or LARK_APP_SECRET is required")
	}

	opts := []Option{
		WithAppID(appID),
		WithAppSecret(appSecret),
	}
	if cfg.EncryptKey != "" {
		opts = append(opts, WithEncryptKey(cfg.EncryptKey))
	}
	if cfg.VerificationToken != "" {
		opts = append(opts, WithVerificationToken(cfg.VerificationToken))
	}
	if cfg.Debug {
		opts = append(opts, WithDebug(true))
	}
	if clearSession != nil {
		opts = append(opts, WithClearSession(clearSession))
	}
	opts = append(opts, extraOpts...)
	return New(opts...), nil
}
