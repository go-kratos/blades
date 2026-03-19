package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-kratos/blades/cmd/blades/internal/config"
	"github.com/go-kratos/blades/cmd/blades/internal/cron"
	"github.com/go-kratos/blades/cmd/blades/internal/workspace"
	"github.com/go-kratos/blades/recipe"
)

type DoctorContext struct {
	Workspace  *workspace.Workspace
	ConfigPath string
	Config     *config.Config
	Getenv     func(string) string
}

type DoctorResult struct {
	Label  string
	Detail string
	OK     bool
	Follow []string
}

type DoctorCheck struct {
	Name string
	Run  func(*DoctorContext) ([]DoctorResult, error)
}

func BuildDoctorContext(opts Options) *DoctorContext {
	bootstrap := NewBootstrap(opts)
	ws := bootstrap.Workspace()
	configPath := ws.ConfigPath()
	if opts.ConfigPath != "" {
		configPath = filepath.Clean(config.ExpandTilde(opts.ConfigPath))
	}
	cfg, _ := bootstrap.LoadConfig()
	return &DoctorContext{
		Workspace:  ws,
		ConfigPath: configPath,
		Config:     cfg,
		Getenv:     os.Getenv,
	}
}

func DefaultDoctorChecks() []DoctorCheck {
	return []DoctorCheck{
		fileDoctorCheck("Blades home (root)", func(ctx *DoctorContext) string { return ctx.Workspace.Home() }),
		fileDoctorCheck("Workspace directory", func(ctx *DoctorContext) string { return ctx.Workspace.WorkspaceDir() }),
		fileDoctorCheck("config.yaml", func(ctx *DoctorContext) string { return ctx.ConfigPath }),
		fileDoctorCheck("agent.yaml", func(ctx *DoctorContext) string { return ctx.Workspace.AgentPath() }),
		fileDoctorCheck("workspace/AGENTS.md", func(ctx *DoctorContext) string { return ctx.Workspace.AgentsPath() }),
		fileDoctorCheck("workspace/SOUL.md", func(ctx *DoctorContext) string { return ctx.Workspace.SoulPath() }),
		fileDoctorCheck("workspace/IDENTITY.md", func(ctx *DoctorContext) string { return ctx.Workspace.IdentityPath() }),
		fileDoctorCheck("workspace/USER.md", func(ctx *DoctorContext) string { return ctx.Workspace.UserPath() }),
		fileDoctorCheck("workspace/MEMORY.md", func(ctx *DoctorContext) string { return ctx.Workspace.MemoryPath() }),
		fileDoctorCheck("workspace/TOOLS.md", func(ctx *DoctorContext) string { return ctx.Workspace.ToolsPath() }),
		fileDoctorCheck("workspace/HEARTBEAT.md", func(ctx *DoctorContext) string { return ctx.Workspace.HeartbeatPath() }),
		fileDoctorCheck("skills/", func(ctx *DoctorContext) string { return ctx.Workspace.SkillsDir() }),
		fileDoctorCheck("workspace/memory/", func(ctx *DoctorContext) string { return ctx.Workspace.MemoriesDir() }),
		providerDoctorCheck(),
		agentRecipeDoctorCheck(),
		execDoctorCheck(),
		larkDoctorCheck(),
		cronDoctorCheck(),
	}
}

func RunDoctorChecks(ctx *DoctorContext, checks []DoctorCheck) ([]DoctorResult, error) {
	var results []DoctorResult
	for _, check := range checks {
		current, err := check.Run(ctx)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", check.Name, err)
		}
		results = append(results, current...)
	}
	return results, nil
}

func PrintDoctorResults(w io.Writer, results []DoctorResult) bool {
	ok := true
	for _, result := range results {
		if result.Label == "stale" {
			fmt.Fprintf(w, "  ✗ %s: %s\n", result.Label, result.Detail)
			ok = false
			continue
		}

		mark := "✓"
		if !result.OK {
			mark = "✗"
			ok = false
		}
		fmt.Fprintf(w, "%s %-30s %s\n", mark, result.Label, result.Detail)
		for _, line := range result.Follow {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
	return ok
}

func fileDoctorCheck(label string, pathFn func(*DoctorContext) string) DoctorCheck {
	return DoctorCheck{
		Name: label,
		Run: func(ctx *DoctorContext) ([]DoctorResult, error) {
			path := pathFn(ctx)
			if _, err := os.Stat(path); err == nil {
				return []DoctorResult{{
					Label:  label,
					Detail: path,
					OK:     true,
				}}, nil
			}
			return []DoctorResult{{
				Label:  label,
				Detail: path + " (missing)",
				OK:     false,
			}}, nil
		},
	}
}

func cronDoctorCheck() DoctorCheck {
	return DoctorCheck{
		Name: "Cron",
		Run: func(ctx *DoctorContext) ([]DoctorResult, error) {
			storePath := ctx.Workspace.CronStorePath()
			if _, err := os.Stat(storePath); err != nil {
				return []DoctorResult{{
					Label:  "Cron",
					Detail: "no cron.yaml (add jobs with 'blades cron add')",
					OK:     true,
				}}, nil
			}

			cronSvc := cron.NewService(storePath, nil)
			jobs, err := cronSvc.ListJobs(false)
			if err != nil {
				return []DoctorResult{{
					Label:  "Cron",
					Detail: err.Error(),
					OK:     false,
				}}, nil
			}

			stale := cronSvc.StaleJobs(26 * time.Hour)
			results := []DoctorResult{{
				Label:  "Cron",
				Detail: fmt.Sprintf("%d jobs, %d stale", len(jobs), len(stale)),
				OK:     true,
			}}
			for _, j := range stale {
				results = append(results, DoctorResult{
					Label:  "stale",
					Detail: cron.FormatJob(j),
					OK:     false,
				})
			}
			return results, nil
		},
	}
}

func providerDoctorCheck() DoctorCheck {
	return DoctorCheck{
		Name: "Providers",
		Run: func(ctx *DoctorContext) ([]DoctorResult, error) {
			if ctx == nil || ctx.Config == nil || len(ctx.Config.Providers) == 0 {
				return []DoctorResult{{
					Label:  "Providers",
					Detail: "none configured",
					OK:     false,
					Follow: []string{"add at least one provider entry to " + ctx.ConfigPath},
				}}, nil
			}

			ok := true
			follow := make([]string, 0, len(ctx.Config.Providers))
			for _, provider := range ctx.Config.Providers {
				entryOK := strings.TrimSpace(provider.APIKey) != "" && len(provider.Models) > 0
				if !entryOK {
					ok = false
				}
				status := "ok"
				switch {
				case strings.TrimSpace(provider.APIKey) == "":
					status = "missing apiKey"
				case len(provider.Models) == 0:
					status = "missing models"
				}
				follow = append(follow, fmt.Sprintf("%s (%s): %s", provider.Name, provider.Provider, status))
			}

			return []DoctorResult{{
				Label:  "Providers",
				Detail: fmt.Sprintf("%d configured", len(ctx.Config.Providers)),
				OK:     ok,
				Follow: follow,
			}}, nil
		},
	}
}

func agentRecipeDoctorCheck() DoctorCheck {
	return DoctorCheck{
		Name: "Agent recipe",
		Run: func(ctx *DoctorContext) ([]DoctorResult, error) {
			spec, err := LoadAgentSpec(ctx.Workspace)
			if err != nil {
				return []DoctorResult{{
					Label:  "agent recipe",
					Detail: err.Error(),
					OK:     false,
				}}, nil
			}
			if err := recipe.Validate(spec); err != nil {
				return []DoctorResult{{
					Label:  "agent recipe",
					Detail: err.Error(),
					OK:     false,
				}}, nil
			}
			return []DoctorResult{{
				Label:  "agent recipe",
				Detail: ctx.Workspace.AgentPath(),
				OK:     true,
			}}, nil
		},
	}
}

func execDoctorCheck() DoctorCheck {
	return DoctorCheck{
		Name: "Tools",
		Run: func(ctx *DoctorContext) ([]DoctorResult, error) {
			execCfg := config.ExecConfig{}
			if ctx != nil && ctx.Config != nil {
				execCfg = ctx.Config.Exec
			}
			ok := execCfg.RestrictToWorkspace
			detail := fmt.Sprintf("timeout=%s", execCfg.ExecTimeout())
			follow := []string{
				fmt.Sprintf("restrictToWorkspace=%t", execCfg.RestrictToWorkspace),
				fmt.Sprintf("allowPatterns=%d denyPatterns=%d", len(execCfg.AllowPatterns), len(execCfg.DenyPatterns)),
			}
			if !ok {
				detail = "workspace restriction disabled"
			}
			return []DoctorResult{{
				Label:  "Tools",
				Detail: detail,
				OK:     ok,
				Follow: follow,
			}}, nil
		},
	}
}

func larkDoctorCheck() DoctorCheck {
	return DoctorCheck{
		Name: "Lark",
		Run: func(ctx *DoctorContext) ([]DoctorResult, error) {
			if ctx == nil || ctx.Config == nil || !ctx.Config.Channels.Lark.Enabled {
				return []DoctorResult{{
					Label:  "Lark",
					Detail: "disabled",
					OK:     true,
				}}, nil
			}

			getenv := os.Getenv
			if ctx.Getenv != nil {
				getenv = ctx.Getenv
			}
			cfg := ctx.Config.Channels.Lark
			appID := strings.TrimSpace(cfg.AppID)
			if appID == "" {
				appID = strings.TrimSpace(getenv("LARK_APP_ID"))
			}
			appSecret := strings.TrimSpace(cfg.AppSecret)
			if appSecret == "" {
				appSecret = strings.TrimSpace(getenv("LARK_APP_SECRET"))
			}
			ok := appID != "" && appSecret != ""
			follow := []string{
				fmt.Sprintf("appID=%t", appID != ""),
				fmt.Sprintf("appSecret=%t", appSecret != ""),
				fmt.Sprintf("encryptKey=%t", strings.TrimSpace(cfg.EncryptKey) != ""),
				fmt.Sprintf("verificationToken=%t", strings.TrimSpace(cfg.VerificationToken) != ""),
			}
			detail := "enabled"
			if !ok {
				detail = "enabled but credentials missing"
			}
			return []DoctorResult{{
				Label:  "Lark",
				Detail: detail,
				OK:     ok,
				Follow: follow,
			}}, nil
		},
	}
}
