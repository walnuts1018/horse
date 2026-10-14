package ingestion

type extractionRetryPolicy struct {
	qualityRun *ExtractionRun
	proReview  bool
}

func extractionRetryPolicyFor(verification Verification, mimeType string, paidProjectConfigured bool) extractionRetryPolicy {
	if verification.Balanced {
		return extractionRetryPolicy{}
	}

	policy := extractionRetryPolicy{}
	if len(verification.Runs) > 0 {
		policy.qualityRun = latestSuccessfulRun(verification.Runs)
	}
	policy.proReview = paidProjectConfigured && supportsProReview(mimeType) && needsProReview(verification.Issues)
	return policy
}

func latestSuccessfulRun(runs []ExtractionRun) *ExtractionRun {
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].Status == extractionStatusSucceeded && runs[i].Receipt != nil {
			return &runs[i]
		}
	}
	return nil
}

func selectVerificationCandidate(current, candidate Verification) Verification {
	if candidate.Balanced != current.Balanced {
		if !candidate.Balanced {
			return current
		}
	} else if len(candidate.Issues) >= len(current.Issues) {
		return current
	}

	current.Receipt = candidate.Receipt
	current.Statements = candidate.Statements
	current.Issues = candidate.Issues
	current.ComputedTotal = candidate.ComputedTotal
	current.Balanced = candidate.Balanced
	current.Candidates = candidate.Candidates
	return current
}
