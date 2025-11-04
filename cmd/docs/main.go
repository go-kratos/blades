package main

import (
	"log"
	"os"

	"github.com/spf13/cobra"
)

var (
	to      string
	model   string
	baseURL string
	apiKey  string
	output  string
)

var (
	rootCmd = &cobra.Command{
		Use:   "docs",
		Short: "Documentation tools",
		Long:  `A set of tools to manage documentation`,
	}
	translateCmd = &cobra.Command{
		Use:   "translate",
		Short: "Translate markdown file",
		Long:  `Translate markdown file using OpenAI API`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if err := translate(args[0]); err != nil {
				log.Fatal(err)
			}
		},
	}
)

func init() {
	translateCmd.Flags().StringVarP(&to, "to", "t", "en", "Target language for translation")
	translateCmd.Flags().StringVarP(&output, "output", "o", "", "Output directory for translated files")
	translateCmd.Flags().StringVarP(&model, "model", "m", os.Getenv("OPENAI_MODEL"), "OpenAI model to use for translation")
	translateCmd.Flags().StringVarP(&baseURL, "base-url", "b", os.Getenv("OPENAI_BASE_URL"), "Base URL for OpenAI API")
	translateCmd.Flags().StringVarP(&apiKey, "api-key", "k", os.Getenv("OPENAI_API_KEY"), "API key for OpenAI")
	rootCmd.AddCommand(translateCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
