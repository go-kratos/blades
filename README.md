## Blades
Blades is a multimodal AI Agent framework for the Go language, supporting custom models, tools, memory, middleware, etc. It is suitable for multi-turn conversations, chain-of-thought reasoning, and structured output, among other use cases.
> The name originates from: The game *God of War*, set against the backdrop of Greek mythology, tells the adventure story of Kratos transforming from a mortal into the God of War and embarking on a god-slaying rampage. The Blades are Kratos's iconic weapons.

## Architecture Design
Blades leverages the characteristics of the Go language to provide a flexible and efficient AI Agent solution. Its core lies in achieving a high degree of decoupling and extensibility through unified interfaces and pluggable components. The overall architecture is as follows:
![architecture](./docs/images/architecture.png)

- Go Idiomatic: Built entirely according to Go's philosophy, with a code style and user experience that feel familiar to Go developers.
- Simple to Use: Define AI Agents through concise code declarations, enabling rapid requirement delivery and making complex logic clear, easy to manage, and maintain.
- Middleware Ecosystem: Drawing inspiration from Kratos's middleware design philosophy, features like Observability and Guardrails can be easily integrated into AI Agents.
- Highly Extensible: Achieves a high degree of decoupling and extensibility through unified interfaces and pluggable components, facilitating the integration of different LLM models and external tools.

## Core Concepts
The Blades framework realizes its powerful functionality and flexibility through a series of carefully designed core components. These components work together to build the intelligent behavior of the Agent:

* Agent: The core unit that executes tasks, capable of invoking models and tools.
* Prompt: Templated text used for interacting with LLMs, supporting dynamic variable substitution and complex context construction.
* Chain: Connects multiple Agents or other Chains to form complex workflows.
* ModelProvider: A pluggable LLM interface, allowing you to easily switch and integrate different language model services (such as OpenAI, etc.).
* Tool: External capabilities that an Agent can use, such as calling APIs, querying databases, accessing the file system, etc.
* Memory: Provides short-term or long-term memory capabilities for the Agent, enabling continuous conversation with context.
* Middleware: Similar to middleware in web frameworks, it enables cross-cutting control over the Agent.

### Agent
`Agent` is the most core interface in the Blades framework, defining the basic behavior of all executable components. Its design aims to provide a unified execution paradigm. Through the `Run` method, it achieves **decoupling, standardization, and high composability** for various functional modules within the framework. Components like `Agent`, `Chain`, and `ModelProvider` all implement this interface, thereby unifying their execution logic and allowing different components to be flexibly combined like Lego bricks to build complex AI Agents.

```go
// Agent represents an autonomous agent that can process invocations and produce a sequence of messages.
type Agent interface {
    Name() string
    Description() string
    Run(context.Context, *Invocation) Generator[*Message, error]
}
```

### ModelProvider
`ModelProvider` is the core abstraction layer in the `Blades` framework for interacting with underlying large language models (LLMs). Its design goal is to achieve **decoupling and extensibility** through a unified interface, separating the framework's core logic from the implementation details of specific models (such as OpenAI, DeepSeek, Gemini, etc.). It acts as an adapter, responsible for converting the framework's internal standardized requests into the format required by the model's native API and converting the model's responses back into the framework's standard format, thus enabling developers to easily switch and integrate different LLMs.

```go
type ModelProvider interface {
    // Generate executes a complete generation request and returns the result all at once. Suitable for scenarios that do not require real-time feedback.
    Generate(context.Context, *ModelRequest, ...ModelOption) (*ModelResponse, error)
    // NewStreaming initiates a streaming request. This method immediately returns a Generator object, allowing the caller to receive the model's generated content step by step. Suitable for building real-time, typewriter-effect conversation applications.
    NewStreaming(context.Context, *ModelRequest, ...ModelOption) (Generator[*ModelResponse])
}
```
![ModelProvider](./docs/images/model.png)

### Agent
`Agent` is the core coordinator in the `Blades` framework. As the top-level `Agent`, it integrates and orchestrates components such as `ModelProvider`, `Tool`, `Memory`, and `Middleware` to understand user intent and execute complex tasks. Its design allows configuration via flexible `Option` functions, thereby driving the behavior and capabilities of intelligent applications and fulfilling core responsibilities like task orchestration, context management, and instruction following.

### Flow
`flow` is used to build complex workflows and multi-step reasoning. Its design philosophy involves orchestrating multiple `Agent` components to achieve data and control flow transfer, where the output of one `Agent` can serve as the input for the next. This mechanism enables developers to flexibly combine components to build highly customized AI workflows, realizing multi-step reasoning and complex data processing. It is key to implementing complex decision-making processes for Agents.

### Tool
`Tool` is a key component for extending AI Agent capabilities, representing external functions or services that an Agent can invoke. Its design aims to empower the Agent to interact with the real world, performing specific actions or obtaining external information. Through a clear `InputSchema`, it guides the LLM to generate correct invocation parameters, and executes the actual logic via its internal `Handle` function, thereby encapsulating various external APIs, database queries, etc., into a form that the Agent can understand and invoke.

### Memory
The `Memory` component endows AI Agents with memory capabilities, providing a universal interface for storing and retrieving conversation messages, ensuring that Agents maintain context and coherence across multiple conversation turns. Its design supports managing messages by session ID and can be configured with message count limits to balance the breadth of memory against system resource consumption. The framework provides an `InMemory` implementation and also encourages developers to extend it to persistent storage or more complex memory strategies.

```go
type Memory interface {
    AddMemory(context.Context, *Memory) error
    SaveSession(context.Context, blades.Session) error
    SearchMemory(context.Context, string) ([]*Memory, error)	
}
```

### Middleware
`Middleware` is a powerful mechanism for implementing cross-cutting concerns (such as logging, monitoring, authentication, rate limiting). Its design allows injecting additional behaviors into the execution flow of a `Runner` without modifying the `Runner`'s core logic. It operates in a function chain form resembling an "onion model," providing highly flexible flow control and feature enhancement, thereby achieving decoupling between non-core business logic and core functionality.

## 💡 Quick Start

### Usage Example (Chat Agent)

The following is a simple chat Agent example demonstrating how to use the OpenAI `ModelProvider` to build a basic conversational application:

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/go-kratos/blades"
    "github.com/go-kratos/blades/contrib/openai"
)

func main() {
    // Configure OpenAI API key and base URL using environment variables:
    model := openai.NewModel("gpt-5", openai.Config{
        APIKey: os.Getenv("OPENAI_API_KEY"),
    })
    agent := blades.NewAgent(
        "Blades Agent",
        blades.WithModel(model),
        blades.WithInstruction("You are a helpful assistant that provides detailed and accurate information."),
    )
    // Create a Prompt with user message
    input := blades.UserMessage("What is the capital of France?")
    // Run the Agent with the Prompt
    runner := blades.NewRunner(agent)
    output, err := runner.Run(context.Background(), input)
    if err != nil {
        log.Fatal(err)
    }
    // Print the agent's response
    log.Println(output.Text())
}
```
For more examples, please refer to the [examples](./examples) directory.

## 🤝 Contribution & Community
The project is currently in its early stages, and we are iterating continuously and rapidly. We sincerely invite all Go developers and AI enthusiasts to visit our GitHub repository and experience the joy of development that Blades brings firsthand.

Welcome to give the project a ⭐️ Star, explore more usage examples in the `examples` directory, or directly start building your first Go LLM application!

We look forward to any feedback, suggestions, and contributions from you to jointly promote the prosperity of the Go AI ecosystem.


## 📄 License

Blades is licensed under the MIT License. For details, please see the [LICENSE](LICENSE) file.
