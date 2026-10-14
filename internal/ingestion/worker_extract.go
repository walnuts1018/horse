package ingestion

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
)

func (w *Worker) extract(ctx context.Context, j job) (Verification, error) {
	router := w.Router
	if w.DB != nil && router.RateControl == nil {
		router.RateControl = postgresGeminiRateControl{db: w.DB}
	}
	var request struct {
		Crop    *Region `json:"crop,omitempty"`
		Purpose string  `json:"purpose,omitempty"`
		Model   string  `json:"model,omitempty"`
	}
	if err := json.Unmarshal(j.Request, &request); err != nil {
		return Verification{}, fmt.Errorf("decode extraction request: %w", err)
	}
	objectKey, mimeType := j.ObjectKey, j.MIMEType
	if request.Crop == nil && j.AnalysisObjectKey != "" && j.AnalysisMIMEType != "" {
		objectKey, mimeType = j.AnalysisObjectKey, j.AnalysisMIMEType
	}
	document, err := w.Store.Get(ctx, objectKey, mimeType)
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
	transform := identityTransform()
	transformPage := 0
	if request.Crop == nil && j.AnalysisTransform != nil {
		transform = *j.AnalysisTransform
		if _, err := transformRegion(Region{Top: 0, Left: 0, Bottom: 1, Right: 1}, transform); err != nil {
			return Verification{}, fmt.Errorf("validate analysis coordinate transform: %w", err)
		}
	}
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
	pipeline := extractionPipeline{router: router, job: j, document: document, hints: hints, lines: lines, ocrErr: ocrErr, transform: transform, page: transformPage}
	initialOptions, err := router.selectedOptions(request.Model, j.SourceKind, document.MIMEType, request.Purpose, j.Attempt)
	if err != nil {
		return Verification{}, err
	}
	verified, runs, err := pipeline.run(ctx, initialOptions, "map evidence regions to original image")
	if err != nil {
		verified.Runs = runs
		return verified, err
	}
	allowProReview := request.Model == "" && w.Router.Paid.APIKey != ""
	if request.Model == ModelSelectionPro {
		return verified, nil
	}
	retryPolicy := extractionRetryPolicyFor(verified, document.MIMEType, allowProReview)
	if retryPolicy.qualityRun != nil {
		guidance := evidenceGuidance(verified.Issues, verified.OCRLines)
		qualityOptions := router.QualityOptions(j.SourceKind, *retryPolicy.qualityRun, guidance)
		qualityOptions.Attempt = j.Attempt
		second, qualityRuns, qualityErr := pipeline.run(ctx, qualityOptions, "map reviewed evidence regions to original image")
		var transformErr *extractionPipelineTransformError
		if errors.As(qualityErr, &transformErr) {
			return Verification{}, qualityErr
		}
		verified.Runs = append(verified.Runs, qualityRuns...)
		if qualityErr == nil {
			verified = selectVerificationCandidate(verified, second)
		}
	}
	retryPolicy = extractionRetryPolicyFor(verified, document.MIMEType, w.Router.Paid.APIKey != "")
	if retryPolicy.proReview {
		proOptions := router.ProOptions(request.Purpose, evidenceGuidance(verified.Issues, verified.OCRLines))
		proOptions.Attempt = j.Attempt
		proVerification, proRuns, proErr := pipeline.run(ctx, proOptions, "map reviewed evidence regions to original image")
		var transformErr *extractionPipelineTransformError
		if errors.As(proErr, &transformErr) {
			return Verification{}, proErr
		}
		verified.Runs = append(verified.Runs, proRuns...)
		if proErr == nil {
			verified = selectVerificationCandidate(verified, proVerification)
		}
	}
	return verified, nil
}

type extractionPipeline struct {
	router    GeminiRouter
	job       job
	document  Document
	hints     []HistoryHint
	lines     []OCRLine
	ocrErr    error
	transform [9]float64
	page      int
}

type extractionPipelineTransformError struct {
	err error
}

func (e *extractionPipelineTransformError) Error() string { return e.err.Error() }

func (e *extractionPipelineTransformError) Unwrap() error { return e.err }

func (p extractionPipeline) run(ctx context.Context, options GeminiRouteOptions, transformError string) (Verification, []ExtractionRun, error) {
	receipt, runs, err := p.router.Generate(ctx, p.job.SourceKind, p.document, p.hints, options)
	if err != nil {
		return Verification{}, runs, err
	}
	verified, err := verifyExtraction(p.job.SourceKind, receipt)
	if err != nil {
		return Verification{Receipt: receipt}, runs, &GeminiAPIError{Kind: GeminiFailureBadResponse, Code: "extraction_validation_failed"}
	}
	if isImageDocument(p.document.MIMEType) {
		if p.ocrErr == nil {
			err = transformVerification(&verified, p.lines, p.transform, p.page)
		} else {
			err = transformReceiptRegions(&verified, p.transform, p.page)
		}
		if err != nil {
			return Verification{}, nil, &extractionPipelineTransformError{err: fmt.Errorf("%s: %w", transformError, err)}
		}
	} else {
		verified.Transform = p.transform
		verified.TransformPage = p.page
	}
	if requiresOCRCorroboration(p.document.MIMEType) {
		verified.OCRLines = p.lines
		applySourceOCRCorroboration(&verified, p.job.SourceKind, p.lines, p.ocrErr)
	}
	verified.Runs = annotateRuns(runs, receipt, verified.Issues)
	return verified, runs, nil
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

func supportsProReview(mimeType string) bool {
	return strings.HasPrefix(mimeType, "image/") || mimeType == mimeTypePDF
}

func isImageDocument(mimeType string) bool {
	return strings.HasPrefix(mimeType, "image/")
}

func requiresOCRCorroboration(mimeType string) bool {
	return isImageDocument(mimeType) || mimeType == mimeTypePDF
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
