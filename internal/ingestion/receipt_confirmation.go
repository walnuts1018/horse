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
		var selected *InterpretationResult
		for _, candidate := range verification.Candidates {
			if candidate.ID != candidateID {
				continue
			}
			selected = &candidate
			break
		}
		if selected == nil {
			return false, errors.New("receipt calculation candidate was not found")
		}
		if reason == "" && !equalReceiptAmounts(amounts, selected.Amounts) {
			return false, errors.New("purchase item amounts do not match receipt calculation candidate")
		}
	} else if len(verification.Receipt.Items) == len(amounts) && reason == "" {
		extractedAmounts := make([]int64, len(verification.Receipt.Items))
		for index, item := range verification.Receipt.Items {
			extractedAmounts[index] = item.GrossAmount
		}
		if !equalReceiptAmounts(amounts, extractedAmounts) {
			return false, errors.New("purchase item amounts do not match extracted receipt amounts")
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

func equalReceiptAmounts(left, right []int64) bool {
	if len(left) != len(right) {
		return false
	}
	for index, amount := range left {
		if amount != right[index] {
			return false
		}
	}
	return true
}
