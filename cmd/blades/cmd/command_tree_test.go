package cmd

import (
	"io"
	"slices"
	"strings"
	"testing"
)

func TestRootCommandTreeIncludesExpectedCommands(t *testing.T) {
	root := newRootCmd()

	if got, want := root.Use, "blades"; got != want {
		t.Fatalf("root.Use = %q, want %q", got, want)
	}
	for _, flagName := range []string{"config", "workspace", "debug"} {
		if root.PersistentFlags().Lookup(flagName) == nil {
			t.Fatalf("expected persistent flag %q to be registered", flagName)
		}
	}

	wantRootSubs := []string{"init", "chat", "run", "memory", "cron", "weixin", "daemon", "doctor"}
	gotRootSubs := subcommandNames(root)
	for _, name := range wantRootSubs {
		if !slices.Contains(gotRootSubs, name) {
			t.Fatalf("root subcommands = %v, want to contain %q", gotRootSubs, name)
		}
	}

	memoryCmd := mustSubcommand(t, root, "memory")
	if got, want := subcommandNames(memoryCmd), []string{"add", "show", "search"}; len(got) != len(want) {
		t.Fatalf("memory subcommands = %v, want %v", got, want)
	} else {
		for _, name := range want {
			if !slices.Contains(got, name) {
				t.Fatalf("memory subcommands = %v, want to contain %q", got, name)
			}
		}
	}

	cronCmd := mustSubcommand(t, root, "cron")
	for _, name := range []string{"list", "add", "heartbeat", "remove", "run"} {
		if !slices.Contains(subcommandNames(cronCmd), name) {
			t.Fatalf("cron subcommands = %v, want to contain %q", subcommandNames(cronCmd), name)
		}
	}

	weixinCmd := mustSubcommand(t, root, "weixin")
	for _, name := range []string{"login", "list"} {
		if !slices.Contains(subcommandNames(weixinCmd), name) {
			t.Fatalf("weixin subcommands = %v, want to contain %q", subcommandNames(weixinCmd), name)
		}
	}
}

func TestRunCommandRequiresMessage(t *testing.T) {
	preserveRootState(t)

	cmd := newRunCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(nil)

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--message is required") {
		t.Fatalf("expected missing message error, got %v", err)
	}
}
