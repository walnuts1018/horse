package ingestion

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"strings"
)

func (w *Worker) extract(ctx context.Context, j job) (Verification, error) {
	router := w.Router
	if w.DB != nil && router.RateControl == nil {
		router.RateControl = postgresGeminiRateControl{db: w.DB}
	}
	document, err := w.Store.Get(ctx, j.ObjectKey, j.MIMEType)
	if err != nil {
		return Verification{}, err
	}
	if document.MIMEType == mimeTypeEmail {
		email, parseErr := parseEmail(document.Data)
		if parseErr == nil && email.Body == "" && len(email.Attachments) > 0 {
			issue := "メール本文が空のため、添付ファイルを個別に確認してください。"
			return Verification{Receipt: Receipt{Warnings: []string{issue}}, Issues: []string{issue}, Balanced: false}, nil
		}
	}
	if document.MIMEType == mimeTypeCSV && (j.SourceKind == statementSourceCard || j.SourceKind == statementSourceBank) {
		entries, err := ParseStatementCSV(document.Data, j.SourceKind)
		if err != nil {
			return Verification{}, err
		}
		return Verification{Statements: entries, Balanced: true}, nil
	}
	var request struct {
		Crop    *Region `json:"crop,omitempty"`
		Purpose string  `json:"purpose,omitempty"`
	}
	if err := json.Unmarshal(j.Request, &request); err != nil {
		return Verification{}, fmt.Errorf("decode extraction request: %w", err)
	}
	transform := identityTransform()
	transformPage := 0
	if request.Crop != nil {
		transformPage = request.Crop.Page
		document, transform, err = w.OCR.Crop(ctx, document, *request.Crop)
		if err != nil {
			return Verification{}, fmt.Errorf("prepare cropped document: %w", err)
		}
	}
	var lines []OCRLine
	var ocrErr error
	if requiresOCRCorroboration(document.MIMEType) {
		lines, ocrErr = w.OCR.Extract(ctx, document)
	}
	hints, err := w.historyHints(ctx, j.HouseholdID, lines)
	if err != nil {
		return Verification{}, fmt.Errorf("load receipt history hints: %w", err)
	}
	receipt, runs, err := router.Generate(ctx, j.SourceKind, document, hints, GeminiRouteOptions{Purpose: request.Purpose, Reason: geminiReasonInitialExtraction, Attempt: j.Attempt})
	if err != nil {
		return Verification{Runs: runs}, err
	}
	verified, err := verifyExtraction(j.SourceKind, receipt)
	if err != nil {
		return Verification{Receipt: receipt, Runs: runs}, &GeminiAPIError{Kind: GeminiFailureBadResponse, Code: "extraction_validation_failed"}
	}
	if isImageDocument(document.MIMEType) {
		if ocrErr == nil {
			if err := transformVerification(&verified, lines, transform, transformPage); err != nil {
				return Verification{}, fmt.Errorf("map evidence regions to original image: %w", err)
			}
		} else if err := transformReceiptRegions(&verified, transform, transformPage); err != nil {
			return Verification{}, fmt.Errorf("map evidence regions to original image: %w", err)
		}
	} else {
		verified.Transform = transform
		verified.TransformPage = transformPage
	}
	if requiresOCRCorroboration(document.MIMEType) {
		verified.OCRLines = lines
		applySourceOCRCorroboration(&verified, j.SourceKind, lines, ocrErr)
	}
	verified.Runs = annotateRuns(runs, receipt, verified.Issues)
	if !verified.Balanced && len(verified.Runs) > 0 {
		guidance := evidenceGuidance(verified.Issues, verified.OCRLines)
		lastRun := latestSuccessfulRun(verified.Runs)
		if lastRun != nil {
			qualityOptions := router.QualityOptions(j.SourceKind, *lastRun, guidance)
			qualityOptions.Attempt = j.Attempt
			alternative, qualityRuns, qualityErr := router.Generate(ctx, j.SourceKind, document, hints, qualityOptions)
			verified.Runs = append(verified.Runs, qualityRuns...)
			if qualityErr == nil {
				second, verifyErr := verifyExtraction(j.SourceKind, alternative)
				if verifyErr == nil {
					if err := transformReceiptRegionsForDocument(&second, document.MIMEType, transform, transformPage); err != nil {
						return Verification{}, fmt.Errorf("map reviewed evidence regions to original image: %w", err)
					}
					second.OCRLines = verified.OCRLines
					if requiresOCRCorroboration(document.MIMEType) {
						applySourceOCRCorroboration(&second, j.SourceKind, second.OCRLines, ocrErr)
					}
					second.Runs = annotateRuns(qualityRuns, alternative, second.Issues)
					if betterVerification(second, verified) {
						verified.Receipt, verified.Statements, verified.Issues, verified.ComputedTotal, verified.Balanced, verified.Candidates = second.Receipt, second.Statements, second.Issues, second.ComputedTotal, second.Balanced, second.Candidates
					}
				}
			}
		}
		if !verified.Balanced && supportsProReview(document.MIMEType) && needsProReview(verified.Issues) && w.Router.Paid.APIKey != "" {
			proOptions := router.ProOptions(request.Purpose, evidenceGuidance(verified.Issues, verified.OCRLines))
			proOptions.Attempt = j.Attempt
			proReceipt, proRuns, proErr := router.Generate(ctx, j.SourceKind, document, hints, proOptions)
			verified.Runs = append(verified.Runs, proRuns...)
			if proErr == nil {
				proVerification, verifyErr := verifyExtraction(j.SourceKind, proReceipt)
				if verifyErr == nil {
					if err := transformReceiptRegionsForDocument(&proVerification, document.MIMEType, transform, transformPage); err != nil {
						return Verification{}, fmt.Errorf("map reviewed evidence regions to original image: %w", err)
					}
					proVerification.OCRLines = verified.OCRLines
					if requiresOCRCorroboration(document.MIMEType) {
						applySourceOCRCorroboration(&proVerification, j.SourceKind, proVerification.OCRLines, ocrErr)
					}
					proVerification.Runs = annotateRuns(proRuns, proReceipt, proVerification.Issues)
					if betterVerification(proVerification, verified) {
						verified.Receipt, verified.Statements, verified.Issues, verified.ComputedTotal, verified.Balanced, verified.Candidates = proVerification.Receipt, proVerification.Statements, proVerification.Issues, proVerification.ComputedTotal, proVerification.Balanced, proVerification.Candidates
					}
				}
			}
		}
	}
	return verified, nil
}

func verifyExtraction(sourceKind string, receipt Receipt) (Verification, error) {
	if sourceKind == statementSourceCard || sourceKind == statementSourceBank {
		verified, err := VerifyStatements(sourceKind, receipt.Statements, receipt.Warnings)
		if err != nil {
			return Verification{}, err
		}
		verified.Receipt = receipt
		verified.Receipt.Statements = verified.Statements
		verified.Receipt.Warnings = verified.Issues
		return verified, nil
	}
	return Verify(receipt)
}

func applySourceOCRCorroboration(verification *Verification, sourceKind string, lines []OCRLine, ocrErr error) {
	if sourceKind != statementSourceCard && sourceKind != statementSourceBank {
		applyOCRCorroboration(verification, lines, ocrErr)
		return
	}
	if ocrErr != nil {
		verification.Issues = append(verification.Issues, "独立したOCR読取を取得できませんでした。原本を確認してください。")
		verification.Balanced = false
		return
	}
	if len(lines) == 0 {
		verification.Issues = append(verification.Issues, "独立したOCR読取に明細行がありません。原本を確認してください。")
		verification.Balanced = false
		return
	}
	verification.Issues = append(verification.Issues, CompareStatementOCR(verification.Statements, lines)...)
	verification.Balanced = len(verification.Issues) == 0
}

func appendPreviousRuns(runs []ExtractionRun, previous []byte) []ExtractionRun {
	if len(previous) == 0 {
		return runs
	}
	var prior Verification
	if err := json.Unmarshal(previous, &prior); err != nil {
		return runs
	}
	return append(prior.Runs, runs...)
}

func annotateRuns(runs []ExtractionRun, receipt Receipt, issues []string) []ExtractionRun {
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].Status == extractionStatusSucceeded && runs[i].Model == receipt.Model && runs[i].Receipt != nil {
			runs[i].Issues = append([]string(nil), issues...)
			break
		}
	}
	return runs
}

func latestSuccessfulRun(runs []ExtractionRun) *ExtractionRun {
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].Status == extractionStatusSucceeded && runs[i].Receipt != nil {
			return &runs[i]
		}
	}
	return nil
}

func betterVerification(candidate, current Verification) bool {
	if candidate.Balanced != current.Balanced {
		return candidate.Balanced
	}
	return len(candidate.Issues) < len(current.Issues)
}

func supportsProReview(mimeType string) bool {
	return strings.HasPrefix(mimeType, "image/") || mimeType == mimeTypePDF
}

func isImageDocument(mimeType string) bool {
	return strings.HasPrefix(mimeType, "image/")
}

func requiresOCRCorroboration(mimeType string) bool {
	return isImageDocument(mimeType) || mimeType == mimeTypePDF
}

func transformReceiptRegionsForDocument(verification *Verification, mimeType string, transform [9]float64, page int) error {
	if isImageDocument(mimeType) {
		return transformReceiptRegions(verification, transform, page)
	}
	verification.Transform = transform
	verification.TransformPage = page
	return nil
}

func applyOCRCorroboration(verification *Verification, lines []OCRLine, ocrErr error) {
	if ocrErr != nil {
		verification.Issues = append(verification.Issues, "独立したOCR読取を取得できませんでした。原本を確認してください。")
		verification.Balanced = false
		return
	}
	if len(lines) == 0 {
		verification.Issues = append(verification.Issues, "独立したOCR読取に明細行がありません。原本を確認してください。")
		verification.Balanced = false
		return
	}
	verification.Issues = append(verification.Issues, CompareOCR(verification.Receipt, lines)...)
	verification.Balanced = len(verification.Issues) == 0
}
