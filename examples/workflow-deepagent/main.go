package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/flow"
	"github.com/go-kratos/blades/tools"
)

// SearchRequest represents a search request
type SearchRequest struct {
	Query string `json:"query" jsonschema:"The search query to execute"`
}

// SearchResponse represents search results
type SearchResponse struct {
	Results []string `json:"results" jsonschema:"List of search results"`
}

// AnalysisRequest represents a data analysis request
type AnalysisRequest struct {
	Data   string `json:"data" jsonschema:"Data to analyze"`
	Format string `json:"format" jsonschema:"Output format (json, markdown, text)"`
}

// AnalysisResponse represents analysis results
type AnalysisResponse struct {
	Summary     string `json:"summary" jsonschema:"Analysis summary"`
	Insights    string `json:"insights" jsonschema:"Key insights"`
	ResultCount int    `json:"result_count" jsonschema:"Number of data points analyzed"`
}

// searchHandler simulates a search tool
func searchHandler(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	log.Printf("Searching for: %s", req.Query)
	results := []string{
		fmt.Sprintf("Result 1 for '%s': NBA Championship records", req.Query),
		fmt.Sprintf("Result 2 for '%s': Career statistics and achievements", req.Query),
		fmt.Sprintf("Result 3 for '%s': Individual awards and honors", req.Query),
		fmt.Sprintf("Result 4 for '%s': Team performance and impact", req.Query),
	}
	return SearchResponse{Results: results}, nil
}

// analyzeDataHandler simulates a data analysis tool
func analyzeDataHandler(ctx context.Context, req AnalysisRequest) (AnalysisResponse, error) {
	log.Printf("Analyzing data in format: %s", req.Format)
	return AnalysisResponse{
		Summary:     fmt.Sprintf("Analysis of %d characters of data", len(req.Data)),
		Insights:    "Key patterns identified: championship wins, MVP awards, scoring records",
		ResultCount: 3,
	}, nil
}

func main() {
	// Create OpenAI model provider
	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		BaseURL: os.Getenv("OPENAI_BASE_URL"),
		APIKey:  os.Getenv("OPENAI_API_KEY"),
	})

	// Step 1: Define custom tools
	// These tools can be used by both the main agent and sub-agents
	searchTool, err := tools.NewFunc(
		"search",
		"Search for information on a given topic",
		searchHandler,
	)
	if err != nil {
		log.Fatal(err)
	}

	analyzeTool, err := tools.NewFunc(
		"analyze_data",
		"Analyze data and generate insights in specified format",
		analyzeDataHandler,
	)
	if err != nil {
		log.Fatal(err)
	}

	// Step 2: Create specialized sub-agents
	// ResearchAgent: Specialized for in-depth research
	researchAgent, err := blades.NewAgent(
		"ResearchAgent",
		blades.WithDescription("Specialized agent for conducting in-depth research on specific topics"),
		blades.WithInstruction("You are a research specialist. Conduct thorough research using available tools and provide comprehensive, well-structured reports."),
		blades.WithModel(model),
		blades.WithTools(searchTool),
	)
	if err != nil {
		log.Fatal(err)
	}

	// DataAnalystAgent: Specialized for data analysis
	dataAnalystAgent, err := blades.NewAgent(
		"DataAnalystAgent",
		blades.WithDescription("Specialized agent for data analysis and generating insights"),
		blades.WithInstruction("You are a data analyst. Analyze data thoroughly and provide actionable insights with clear visualizations when possible."),
		blades.WithModel(model),
		blades.WithTools(analyzeTool),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Step 3: Configure DeepAgent
	config := flow.DeepConfig{
		Name:          "ResearchCoordinator",
		Model:         model,
		Description:   "An intelligent coordinator that decomposes complex research and analysis tasks into manageable subtasks",
		Instruction:   "You are an expert research coordinator. You excel at breaking down complex multi-step tasks into smaller, focused subtasks. Use the write_todos tool to plan complex tasks, and delegate specialized work to appropriate sub-agents using the task tool.",
		Tools:         []tools.Tool{searchTool, analyzeTool},
		SubAgents:     []blades.Agent{researchAgent, dataAnalystAgent},
		MaxIterations: 20,
	}

	agent, err := flow.NewDeepAgent(config)
	if err != nil {
		log.Fatal(err)
	}

	// Create runner
	runner := blades.NewRunner(agent)

	input := blades.UserMessage("I want to conduct research on the accomplishments of LeBron James, Michael Jordan, and Kobe Bryant, then compare them. Use the write_todos tool to plan this task and the task tool to delegate research to specialized agents.")
	output, err := runner.Run(context.Background(), input)
	if err != nil {
		log.Fatal(err)
	}
	log.Println(output.Text())
}
