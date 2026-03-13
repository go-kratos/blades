package main

import (
	"fmt"

	"github.com/spf13/cobra"
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
	return &cobra.Command{
		Use:   "add <text>",
		Short: "Append text to MEMORY.md",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, mem, err := loadAll()
			if err != nil {
				return err
			}
			if err := mem.AppendMemory(args[0]); err != nil {
				return err
			}
			fmt.Println("✓ appended to MEMORY.md")
			return nil
		},
	}
}

func newMemoryShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Display MEMORY.md",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, mem, err := loadAll()
			if err != nil {
				return err
			}
			content, err := mem.ReadMemory()
			if err != nil {
				return err
			}
			fmt.Println(content)
			return nil
		},
	}
}

func newMemorySearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <query>",
		Short: "Search historical session logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, _, mem, err := loadAll()
			if err != nil {
				return err
			}
			results, err := mem.SearchLogs(args[0])
			if err != nil {
				return err
			}
			if len(results) == 0 {
				fmt.Println("(no results)")
				return nil
			}
			for _, r := range results {
				fmt.Println(r)
			}
			return nil
		},
	}
}
