package ingestion

import (
	"errors"
	"strings"
)

func ValidateReceiptConfirmation(verification Verification, amounts []int64, candidateID, overrideReason string) (bool, error) {
	reason := strings.TrimSpace(overrideReason)
	if len(reason) > 500 {
		return false, errors.New("receipt override reason is too long")
	}
	if len(amounts) == 0 {
		return false, errors.New("purchase items are required")
	}

	if candidateID != "" {
		found := false
		for _, candidate := range verification.Candidates {
			if candidate.ID != candidateID {
				continue
			}
			found = true
			break
		}
		if !found {
			return false, errors.New("receipt calculation candidate was not found")
		}
	}

	var total int64
	for _, amount := range amounts {
		if amount < 0 || amount > MaxAmount-total {
			return false, errors.New("purchase item amount is invalid")
		}
		total += amount
	}
	if total != verification.Receipt.Total && reason == "" {
		return false, errors.New("purchase total does not match receipt total")
	}
	return reason != "", nil
}
