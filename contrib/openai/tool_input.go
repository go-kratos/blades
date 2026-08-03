package openai

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/go-kratos/blades/jsonrepair"
)

func normalizeToolInputJSON(input []byte, repairer jsonrepair.Repairer) ([]byte, error) {
	if len(input) == 0 {
		input = []byte("{}")
	}
	if json.Valid(input) {
		return append([]byte(nil), input...), nil
	}

	var decoded any
	validationErr := json.Unmarshal(input, &decoded)
	if validationErr == nil {
		validationErr = errors.New("invalid JSON")
	}
	if repairer == nil {
		return nil, validationErr
	}

	repaired, repairErr := repairer.Repair(input)
	if repairErr != nil {
		return nil, fmt.Errorf("%v; repair failed: %w", validationErr, repairErr)
	}
	if !json.Valid(repaired.JSON) {
		return nil, fmt.Errorf("%v; repair produced invalid JSON", validationErr)
	}
	return append([]byte(nil), repaired.JSON...), nil
}
