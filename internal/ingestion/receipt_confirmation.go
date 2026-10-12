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
	if len(amounts) != len(verification.Receipt.Items) || len(amounts) == 0 {
		return false, errors.New("purchase items do not match receipt items")
	}

	var expected []int64
	verified := candidateID == "" && verification.Balanced
	if candidateID != "" {
		found := false
		for _, candidate := range verification.Candidates {
			if candidate.ID != candidateID {
				continue
			}
			found = true
			if candidate.Consistent && len(candidate.Amounts) == len(amounts) {
				expected = candidate.Amounts
				verified = true
			}
			break
		}
		if !found {
			return false, errors.New("receipt calculation candidate was not found")
		}
	} else if verification.Balanced {
		expected = make([]int64, len(verification.Receipt.Items))
		for index, item := range verification.Receipt.Items {
			expected[index] = item.GrossAmount
		}
	}

	matched := verified && len(expected) == len(amounts)
	if matched {
		for index, amount := range amounts {
			if amount != expected[index] {
				matched = false
				break
			}
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
	if !matched && reason == "" {
		return false, errors.New("receipt amounts require a verified calculation or override reason")
	}
	return reason != "", nil
}
