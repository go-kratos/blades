package cmd

import (
	"context"
	"time"

	wx "github.com/daemon365/weixin-clawbot"
	"github.com/spf13/cobra"

	weixinch "github.com/go-kratos/blades/cmd/blades/internal/channel/weixin"
)

func newWeixinCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "weixin",
		Short: "Manage Weixin/iLink accounts",
	}
	cmd.AddCommand(newWeixinLoginCmd(), newWeixinListCmd())
	return cmd
}

func newWeixinLoginCmd() *cobra.Command {
	var (
		baseURL     string
		botType     string
		routeTag    string
		saveDir     string
		accountHint string
		timeout     time.Duration
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Login to Weixin by QR code and save the account locally",
		RunE: func(cmd *cobra.Command, args []string) error {
			if saveDir == "" {
				saveDir = weixinch.DefaultAccountDir()
			}
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			client := wx.NewClient(wx.Options{
				BaseURL:  baseURL,
				BotType:  botType,
				RouteTag: routeTag,
				Output:   commandOut(cmd),
			})
			account, err := client.LoginInteractive(ctx, wx.InteractiveLoginOptions{
				AccountHint: accountHint,
				Timeout:     timeout,
				Output:      commandOut(cmd),
				SaveDir:     saveDir,
			})
			if err != nil {
				return err
			}

			printCommandf(cmd, "saved weixin account: %s\n", account.AccountID)
			printCommandf(cmd, "account dir: %s\n", saveDir)
			printCommandf(cmd, "sync dir: %s\n", weixinch.DefaultSyncDir())
			printCommandln(cmd, "config hint: set channels.weixin.enabled: true")
			return nil
		},
	}
	cmd.Flags().StringVar(&baseURL, "base-url", wx.DefaultBaseURL, "iLink base URL")
	cmd.Flags().StringVar(&botType, "bot-type", wx.DefaultBotType, "iLink bot_type")
	cmd.Flags().StringVar(&routeTag, "route-tag", "", "optional SKRouteTag header")
	cmd.Flags().StringVar(&saveDir, "save-dir", "", "directory to persist scanned weixin accounts (default: ~/.blades/weixin/account)")
	cmd.Flags().StringVar(&accountHint, "account-hint", "", "optional account hint shown during login")
	cmd.Flags().DurationVar(&timeout, "timeout", wx.DefaultLoginTimeout, "overall login timeout")
	return cmd
}

func newWeixinListCmd() *cobra.Command {
	var saveDir string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List saved Weixin accounts",
		RunE: func(cmd *cobra.Command, args []string) error {
			if saveDir == "" {
				saveDir = weixinch.DefaultAccountDir()
			}

			accounts, err := weixinch.ListSavedAccounts(saveDir)
			if err != nil {
				return err
			}
			if len(accounts) == 0 {
				printCommandf(cmd, "no weixin accounts found in %s\n", saveDir)
				return nil
			}

			printCommandf(cmd, "weixin accounts in %s:\n", saveDir)
			for _, account := range accounts {
				printCommandf(cmd, "- %s", account.AccountID)
				if account.UserID != "" {
					printCommandf(cmd, " user=%s", account.UserID)
				}
				if account.SavedAt != "" {
					printCommandf(cmd, " saved_at=%s", account.SavedAt)
				}
				if account.BaseURL != "" {
					printCommandf(cmd, " base_url=%s", account.BaseURL)
				}
				printCommandln(cmd)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&saveDir, "save-dir", "", "directory containing saved weixin accounts (default: ~/.blades/weixin/account)")
	return cmd
}
