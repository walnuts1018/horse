package api

import (
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type documentInteractionEvent struct {
	Status string `json:"status"`
	Reply  string `json:"reply"`
}

func (s *Server) documentInteractionEvents(w http.ResponseWriter, r *http.Request) {
	principal, err := s.authenticate(r)
	if err != nil {
		s.writeAuthenticationFailure(w, r, err)
		return
	}
	documentID, err := uuid.FromString(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	jobID, err := uuid.FromString(r.PathValue("jobId"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	memberID, err := uuid.FromString(principal.MemberID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeOperationalError(w, r, "document interaction stream unavailable", http.StatusInternalServerError, errors.New("response writer does not support streaming"))
		return
	}
	event, terminal, err := s.currentDocumentInteractionEvent(r, documentID, jobID, memberID)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.writeOperationalError(w, r, "document interaction stream lookup failed", http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var previous documentInteractionEvent
	for {
		if event != previous {
			payload, err := json.Marshal(event)
			if err != nil {
				s.logger.ErrorContext(r.Context(), "document interaction stream encoding failed", "error", err)
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				s.logger.DebugContext(r.Context(), "document interaction stream disconnected", "error", err)
				return
			}
			flusher.Flush()
			previous = event
		} else {
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				s.logger.DebugContext(r.Context(), "document interaction stream disconnected", "error", err)
				return
			}
			flusher.Flush()
		}
		if terminal {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
		event, terminal, err = s.currentDocumentInteractionEvent(r, documentID, jobID, memberID)
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				s.logger.ErrorContext(r.Context(), "document interaction stream refresh failed", "error", err)
			}
			return
		}
	}
}

func (s *Server) currentDocumentInteractionEvent(r *http.Request, documentID, jobID, memberID uuid.UUID) (documentInteractionEvent, bool, error) {
	executor := bobpgx.NewPool(s.db)
	document, err := documentForMember(r.Context(), executor, documentID, memberID, false)
	if err != nil {
		return documentInteractionEvent{}, false, err
	}
	job, err := models.ExtractionJobs.Query(sm.Where(psql.And(
		models.ExtractionJobs.Columns.ID.EQ(psql.Arg(jobID)),
		models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(document.ID)),
		models.ExtractionJobs.Columns.HouseholdID.EQ(psql.Arg(document.HouseholdID)),
	))).One(r.Context(), executor)
	if err != nil {
		return documentInteractionEvent{}, false, err
	}
	var request struct {
		Purpose       string `json:"purpose"`
		Clarification string `json:"clarification"`
	}
	if err := json.Unmarshal(job.Request.Val, &request); err != nil || request.Purpose != ingestion.JobPurposeInteractive || request.Clarification == "" {
		return documentInteractionEvent{}, false, sql.ErrNoRows
	}
	switch job.Status {
	case "queued", "running", "succeeded", "failed":
	default:
		return documentInteractionEvent{}, false, sql.ErrNoRows
	}
	event := documentInteractionEvent{Status: job.Status, Reply: job.InteractionReplyPreview}
	if job.Status == "failed" || job.Status == "succeeded" {
		event.Reply = ""
	}
	return event, job.Status != "queued" && job.Status != "running", nil
}
