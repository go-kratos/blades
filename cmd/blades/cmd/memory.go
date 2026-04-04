package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/go-kratos/blades/cmd/blades/internal/memory"
)

func newMemoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Manage long-term memory (MEMORY.md)",
	}
	cmd.AddCommand(
		newMemoryAddCmd(),
		newMemoryShowCmd(),
		newMemorySearchCmd(),
	)
	return cmd
}

func newMemoryAddCmd() *cobra.Command {
	return newMemoryActionCmd("add <text>", "Append text to MEMORY.md", cobra.ExactArgs(1), func(cmd *cobra.Command, mem *memory.Store, args []string) error {
		if err := mem.AppendMemory(args[0]); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "✓ appended to MEMORY.md")
		return nil
	})
}

func newMemoryShowCmd() *cobra.Command {
	return newMemoryActionCmd("show", "Display MEMORY.md", cobra.NoArgs, func(cmd *cobra.Command, mem *memory.Store, args []string) error {
		content, err := mem.ReadMemory()
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), content)
		return nil
	})
}

func newMemorySearchCmd() *cobra.Command {
	return newMemoryActionCmd("search <query>", "Search historical session logs", cobra.ExactArgs(1), func(cmd *cobra.Command, mem *memory.Store, args []string) error {
		results, err := mem.SearchLogs(args[0])
		if err != nil {
			return err
		}
		if len(results) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "(no results)")
			return nil
		}
		for _, r := range results {
			fmt.Fprintln(cmd.OutOrStdout(), r)
		}
		return nil
	})
}
