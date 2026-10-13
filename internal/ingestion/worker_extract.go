package ingestion

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"strings"
)

func (w *Worker) extract(ctx context.Context, j job) (Verification, error) {
	document, err := w.Store.Get(ctx, j.ObjectKey, j.MIMEType)
	if err != nil {
		return Verification{}, err
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
	if strings.HasPrefix(document.MIMEType, "image/") {
		lines, ocrErr = w.OCR.Extract(ctx, document)
	}
	hints, err := w.historyHints(ctx, j.HouseholdID, lines)
	if err != nil {
		return Verification{}, fmt.Errorf("load receipt history hints: %w", err)
	}
	receipt, runs, err := w.Router.Generate(ctx, j.SourceKind, document, hints, GeminiRouteOptions{Purpose: request.Purpose, Reason: geminiReasonInitialExtraction})
	if err != nil {
		return Verification{Runs: runs}, err
	}
	verified, err := Verify(receipt)
	if err != nil {
		return Verification{Receipt: receipt, Runs: runs}, &GeminiAPIError{Kind: GeminiFailureBadResponse, Code: "receipt_validation_failed"}
	}
	if len(document.MIMEType) >= 6 && document.MIMEType[:6] == "image/" {
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
	verified.Runs = annotateRuns(runs, receipt, verified.Issues)
	if len(document.MIMEType) >= 6 && document.MIMEType[:6] == "image/" {
		if ocrErr != nil {
			verified.Issues = append(verified.Issues, "独立したOCR読取を取得できませんでした。原本を確認してください。")
		} else {
			verified.OCRLines = lines
			verified.Issues = append(verified.Issues, CompareOCR(verified.Receipt, lines)...)
		}
		verified.Balanced = len(verified.Issues) == 0
	}
	if !verified.Balanced && len(verified.Runs) > 0 {
		guidance := evidenceGuidance(verified.Issues, verified.OCRLines)
		lastRun := latestSuccessfulRun(verified.Runs)
		if lastRun != nil {
			alternative, qualityRuns, qualityErr := w.Router.Generate(ctx, j.SourceKind, document, hints, w.Router.QualityOptions(j.SourceKind, *lastRun, guidance))
			verified.Runs = append(verified.Runs, qualityRuns...)
			if qualityErr == nil {
				second, verifyErr := Verify(alternative)
				if verifyErr == nil {
					if err := transformReceiptRegions(&second, transform, transformPage); err != nil {
						return Verification{}, fmt.Errorf("map reviewed evidence regions to original image: %w", err)
					}
					second.Runs = annotateRuns(qualityRuns, alternative, second.Issues)
					second.OCRLines = verified.OCRLines
					if len(second.OCRLines) > 0 {
						second.Issues = append(second.Issues, CompareOCR(second.Receipt, second.OCRLines)...)
						second.Balanced = len(second.Issues) == 0
					}
					if betterVerification(second, verified) {
						verified.Receipt, verified.Issues, verified.ComputedTotal, verified.Balanced, verified.Candidates = second.Receipt, second.Issues, second.ComputedTotal, second.Balanced, second.Candidates
					}
				}
			}
		}
		if !verified.Balanced && supportsProReview(document.MIMEType) && needsProReview(verified.Issues) && w.Router.Paid.APIKey != "" {
			proReceipt, proRuns, proErr := w.Router.Generate(ctx, j.SourceKind, document, hints, w.Router.ProOptions(request.Purpose, evidenceGuidance(verified.Issues, verified.OCRLines)))
			verified.Runs = append(verified.Runs, proRuns...)
			if proErr == nil {
				proVerification, verifyErr := Verify(proReceipt)
				if verifyErr == nil {
					if err := transformReceiptRegions(&proVerification, transform, transformPage); err != nil {
						return Verification{}, fmt.Errorf("map reviewed evidence regions to original image: %w", err)
					}
					proVerification.Runs = annotateRuns(proRuns, proReceipt, proVerification.Issues)
					proVerification.OCRLines = verified.OCRLines
					if len(proVerification.OCRLines) > 0 {
						proVerification.Issues = append(proVerification.Issues, CompareOCR(proVerification.Receipt, proVerification.OCRLines)...)
						proVerification.Balanced = len(proVerification.Issues) == 0
					}
					if betterVerification(proVerification, verified) {
						verified.Receipt, verified.Issues, verified.ComputedTotal, verified.Balanced, verified.Candidates = proVerification.Receipt, proVerification.Issues, proVerification.ComputedTotal, proVerification.Balanced, proVerification.Candidates
					}
				}
			}
		}
	}
	return verified, nil
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
