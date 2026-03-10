package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/go-kratos/blades"
	"github.com/go-kratos/blades/contrib/openai"
	"github.com/go-kratos/blades/tools"
)

const (
	leaveApprovalKey    = "leave_approval"
	defaultLeaveRequest = "My name is Alice. I need leave from 2026-03-12 to 2026-03-14 because I have a fever."
)

type LeaveApprovalRequest struct {
	EmployeeName string `json:"employee_name" jsonschema:"Employee name"`
	StartDate    string `json:"start_date" jsonschema:"Leave start date in YYYY-MM-DD format"`
	EndDate      string `json:"end_date" jsonschema:"Leave end date in YYYY-MM-DD format"`
	Reason       string `json:"reason" jsonschema:"Reason for the leave request"`
}

type LeaveApprovalResult struct {
	Approved bool `json:"approved" jsonschema:"Whether the leave request is approved"`
}

type LeaveApprovalState struct {
	Request  LeaveApprovalRequest
	Decision string
}

func requestLeaveApproval(ctx context.Context, req LeaveApprovalRequest) (LeaveApprovalResult, error) {
	session, ok := blades.FromSessionContext(ctx)
	if !ok {
		return LeaveApprovalResult{}, blades.ErrNoSessionContext
	}

	state, _ := session.State()[leaveApprovalKey].(LeaveApprovalState)
	state.Request = req
	session.SetState(leaveApprovalKey, state)

	if state.Decision == "" {
		return LeaveApprovalResult{}, blades.ErrInterrupted
	}

	return LeaveApprovalResult{
		Approved: state.Decision == "approve",
	}, nil
}

func promptApproval(session blades.Session) error {
	state, ok := session.State()[leaveApprovalKey].(LeaveApprovalState)
	if !ok {
		return fmt.Errorf("pending leave request not found in session")
	}
	req := state.Request
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Leave request is waiting for human approval:")
	fmt.Printf("  Employee: %s\n", req.EmployeeName)
	fmt.Printf("  Start:    %s\n", req.StartDate)
	fmt.Printf("  End:      %s\n", req.EndDate)
	fmt.Printf("  Reason:   %s\n", req.Reason)

	for {
		fmt.Print("Decision [approve/reject]: ")
		decision, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		decision = strings.TrimSpace(decision)
		if decision != "approve" && decision != "reject" {
			fmt.Println("Please enter either approve or reject.")
			continue
		}

		state.Decision = decision
		session.SetState(leaveApprovalKey, state)
		return nil
	}
}

func main() {
	approvalTool, err := tools.NewFunc(
		"request_leave_approval",
		"Submit a leave request for human approval. Always call this tool before answering a leave request.",
		requestLeaveApproval,
	)
	if err != nil {
		log.Fatal(err)
	}

	model := openai.NewModel(os.Getenv("OPENAI_MODEL"), openai.Config{
		APIKey: os.Getenv("OPENAI_API_KEY"),
	})
	agent, err := blades.NewAgent(
		"LeaveApprovalAgent",
		blades.WithModel(model),
		blades.WithInstruction(`You are an HR leave assistant.
For every leave request:
1. Extract employee_name, start_date, end_date, and reason.
2. Call the request_leave_approval tool exactly once.
3. After the tool returns, tell the user whether the leave was approved or rejected.`),
		blades.WithTools(approvalTool),
	)
	if err != nil {
		log.Fatal(err)
	}

	input := blades.UserMessage(defaultLeaveRequest)
	ctx := context.Background()
	session := blades.NewSession()
	invocationID := "leave-approval-001"
	runner := blades.NewRunner(agent)

	output, err := runner.Run(
		ctx,
		input,
		blades.WithSession(session),
		blades.WithInvocationID(invocationID),
	)
	if err != nil {
		if !errors.Is(err, blades.ErrInterrupted) {
			log.Fatal(err)
		}
	}
	log.Println("leave request paused, waiting for human approval")
	if err := promptApproval(session); err != nil {
		log.Fatal(err)
	}
	output, err = runner.Run(
		ctx,
		input,
		blades.WithResume(true),
		blades.WithSession(session),
		blades.WithInvocationID(invocationID),
	)
	if err != nil {
		log.Fatal(err)
	}

	log.Println(output.Author, output.Text())
}
