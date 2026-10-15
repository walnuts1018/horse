package api

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"encoding/json/v2"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

const maxDocumentConversationMessages = 40

func (s *Server) interactWithDocument(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(w, r) {
		return
	}
	principal, err := s.authenticate(r)
	if err != nil {
		s.writeAuthenticationFailure(w, r, err)
		return
	}
	if s.cfg.Store == nil {
		http.Error(w, "document processing is unavailable", http.StatusServiceUnavailable)
		return
	}
	var input struct {
		Message string `json:"message"`
		Model   string `json:"model,omitempty"`
	}
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, 64<<10), &input); err != nil || !utf8.ValidString(input.Message) || strings.TrimSpace(input.Message) == "" || len(input.Message) > 8_000 || input.Model != "" && input.Model != ingestion.ModelPro {
		http.Error(w, "invalid document interaction input", http.StatusBadRequest)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.logger.ErrorContext(r.Context(), "document interaction transaction failed", "error", err)
		http.Error(w, "document interaction failed", http.StatusInternalServerError)
		return
	}
	defer func(ctx context.Context) { _ = tx.Rollback(ctx) }(context.WithoutCancel(r.Context()))
	documentID, err := uuid.FromString(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	memberID, err := uuid.FromString(principal.MemberID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	executor := bobpgx.NewTx(tx, nil)
	document, err := documentForMember(r.Context(), executor, documentID, memberID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.logger.ErrorContext(r.Context(), "document interaction document lookup failed", "error", err)
		http.Error(w, "document lookup failed", http.StatusInternalServerError)
		return
	}
	latestJob, err := models.ExtractionJobs.Query(
		sm.Where(models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(document.ID))),
		sm.OrderBy(models.ExtractionJobs.Columns.Version).Desc(), sm.Limit(1), sm.ForUpdate(),
	).One(r.Context(), executor)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "document interaction requires an existing extraction", http.StatusConflict)
		return
	}
	if err != nil {
		s.logger.ErrorContext(r.Context(), "document interaction extraction lookup failed", "error", err)
		http.Error(w, "extraction lookup failed", http.StatusInternalServerError)
		return
	}
	if latestJob.Status == "running" || latestJob.Status == "queued" {
		http.Error(w, "document extraction is already running", http.StatusConflict)
		return
	}
	latestSuccess, err := models.ExtractionJobs.Query(
		sm.Where(psql.And(
			models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(document.ID)),
			models.ExtractionJobs.Columns.HouseholdID.EQ(psql.Arg(document.HouseholdID)),
			models.ExtractionJobs.Columns.Status.EQ(psql.Arg("succeeded")),
		)),
		sm.OrderBy(models.ExtractionJobs.Columns.Version).Desc(), sm.Limit(1),
	).One(r.Context(), executor)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !latestSuccess.Result.Valid {
		http.Error(w, "document interaction requires an existing extraction", http.StatusConflict)
		return
	}
	if err != nil {
		s.logger.ErrorContext(r.Context(), "document interaction extraction lookup failed", "error", err)
		http.Error(w, "extraction lookup failed", http.StatusInternalServerError)
		return
	}
	var previous ingestion.Verification
	if err := stdjson.Unmarshal(latestSuccess.Result.V.Val, &previous); err != nil {
		s.logger.ErrorContext(r.Context(), "document interaction result decoding failed", "error", err)
		http.Error(w, "extraction result is unavailable", http.StatusConflict)
		return
	}
	if len(previous.Receipt.Items) == 0 && len(previous.Statements) == 0 {
		http.Error(w, "document interaction requires receipt or statement data", http.StatusConflict)
		return
	}
	if len(previous.Conversation) > maxDocumentConversationMessages-2 {
		http.Error(w, "document conversation reached its turn limit", http.StatusConflict)
		return
	}
	if err := validateDocumentConversation(previous.Conversation); err != nil {
		http.Error(w, "document conversation is unavailable", http.StatusConflict)
		return
	}
	conversationID := previous.ConversationID
	if conversationID == "" {
		conversationIDValue, idErr := uuid.NewV7()
		if idErr != nil {
			s.logger.ErrorContext(r.Context(), "document interaction ID generation failed", "error", idErr)
			http.Error(w, "document interaction failed", http.StatusInternalServerError)
			return
		}
		conversationID = conversationIDValue.String()
	}
	var previousRequest struct {
		Model string `json:"model,omitempty"`
	}
	if err := stdjson.Unmarshal(latestSuccess.Request.Val, &previousRequest); err != nil || !validDocumentModelSelection(previousRequest.Model) {
		http.Error(w, "extraction model is unavailable", http.StatusConflict)
		return
	}
	model, _ := documentModelSelection(previousRequest.Model)
	if input.Model != "" {
		model = input.Model
	}
	currentExtraction, err := stdjson.Marshal(struct {
		Receipt    ingestion.Receipt          `json:"receipt"`
		Statements []ingestion.StatementEntry `json:"statements,omitempty"`
		Issues     []string                   `json:"issues,omitempty"`
	}{Receipt: previous.Receipt, Statements: previous.Statements, Issues: previous.Issues})
	if err != nil {
		s.logger.ErrorContext(r.Context(), "document interaction extraction encoding failed", "error", err)
		http.Error(w, "extraction result is unavailable", http.StatusInternalServerError)
		return
	}
	request, err := stdjson.Marshal(struct {
		Purpose           string                          `json:"purpose"`
		Model             string                          `json:"model"`
		Clarification     string                          `json:"clarification"`
		ConversationID    string                          `json:"conversationId"`
		Conversation      []ingestion.ConversationMessage `json:"conversation,omitempty"`
		CurrentExtraction stdjson.RawMessage              `json:"currentExtraction"`
	}{
		Purpose: ingestion.JobPurposeInteractive, Model: model, Clarification: input.Message, ConversationID: conversationID,
		Conversation: previous.Conversation, CurrentExtraction: currentExtraction,
	})
	if err != nil {
		s.logger.ErrorContext(r.Context(), "document interaction request encoding failed", "error", err)
		http.Error(w, "document interaction failed", http.StatusInternalServerError)
		return
	}
	jobID, err := uuid.NewV7()
	if err != nil {
		s.logger.ErrorContext(r.Context(), "document interaction job ID generation failed", "error", err)
		http.Error(w, "document interaction failed", http.StatusInternalServerError)
		return
	}
	version := latestJob.Version + 1
	status := documentStatusQueued
	requestJSON := types.JSON[stdjson.RawMessage]{Val: request}
	if _, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{
		ID: &jobID, HouseholdID: &document.HouseholdID, SourceDocumentID: &document.ID, Version: &version,
		Status: &status, Request: &requestJSON,
	}).Exec(r.Context(), executor); err != nil {
		s.logger.ErrorContext(r.Context(), "document interaction job insertion failed", "error", err)
		http.Error(w, "document interaction failed", http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.logger.ErrorContext(r.Context(), "document interaction transaction commit failed", "error", err)
		http.Error(w, "document interaction failed", http.StatusInternalServerError)
		return
	}
	if s.cfg.Queue != nil {
		if err := s.cfg.Queue.Publish(r.Context(), jobID.String()); err != nil {
			s.logger.WarnContext(r.Context(), "extraction job notification failed", "job_id", jobID.String(), "error", err)
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]string{jobIDJSONKey: jobID.String(), "status": documentStatusQueued})
}

func validateDocumentConversation(messages []ingestion.ConversationMessage) error {
	if len(messages)%2 != 0 || len(messages) > maxDocumentConversationMessages {
		return errors.New("invalid message count")
	}
	for index, message := range messages {
		role := "user"
		if index%2 == 1 {
			role = "assistant"
		}
		if message.Role != role || message.Text == "" || len(message.Text) > 12_000 || !utf8.ValidString(message.Text) {
			return errors.New("invalid conversation message")
		}
	}
	return nil
}
