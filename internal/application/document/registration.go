package document

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"errors"
	"uuid"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type StoredEmailAttachment struct {
	Filename  string
	MIMEType  string
	ObjectKey string
	SHA256    string
}

func (s *Service) RegisterEmailAttachments(ctx context.Context, executor bob.Executor, householdID, memberID, parentID gofrsuuid.UUID, purpose string, attachments []StoredEmailAttachment) ([]gofrsuuid.UUID, error) {
	queued := make([]gofrsuuid.UUID, 0, len(attachments))
	seen := make(map[gofrsuuid.UUID]struct{}, len(attachments))
	for index, attachment := range attachments {
		childID, jobID, err := s.registerAttachmentDocument(ctx, executor, householdID, memberID, attachment, purpose)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[childID]; exists {
			continue
		}
		seen[childID] = struct{}{}
		attachmentIndex := int32(index)
		if _, err := models.SourceDocumentAttachments.Insert(&models.SourceDocumentAttachmentSetter{
			HouseholdID: &householdID, ParentDocumentID: &parentID, ChildDocumentID: &childID,
			AttachmentIndex: &attachmentIndex, Filename: &attachment.Filename, MimeType: &attachment.MIMEType,
		}, im.OnConflict("parent_document_id", "attachment_index").DoNothing()).Exec(ctx, executor); err != nil {
			return nil, err
		}
		if jobID != nil {
			queued = append(queued, *jobID)
		}
	}
	return queued, nil
}

func (s *Service) registerAttachmentDocument(ctx context.Context, executor bob.Executor, householdID, memberID gofrsuuid.UUID, attachment StoredEmailAttachment, purpose string) (gofrsuuid.UUID, *gofrsuuid.UUID, error) {
	id, err := gofrsuuid.FromString(uuid.NewV7().String())
	if err != nil {
		return gofrsuuid.Nil, nil, err
	}
	sourceKind := "receipt"
	filename := sql.Null[string]{V: attachment.Filename, Valid: true}
	document, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{
		ID: &id, HouseholdID: &householdID, Sha256: &attachment.SHA256, ObjectKey: &attachment.ObjectKey, MimeType: &attachment.MIMEType,
		SourceKind: &sourceKind, SourceFilename: &filename, CreatedBy: &memberID,
	}, im.OnConflict("household_id", "sha256").DoNothing()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		document, err = models.SourceDocuments.Query(sm.Where(psql.And(
			models.SourceDocuments.Columns.HouseholdID.EQ(psql.Arg(householdID)),
			models.SourceDocuments.Columns.Sha256.EQ(psql.Arg(attachment.SHA256)),
		))).One(ctx, executor)
	}
	if err != nil {
		return gofrsuuid.Nil, nil, err
	}
	_, err = models.ExtractionJobs.Query(
		sm.Where(models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(document.ID))),
		sm.OrderBy(models.ExtractionJobs.Columns.Version).Desc(), sm.Limit(1),
	).One(ctx, executor)
	if err == nil {
		return document.ID, nil, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return gofrsuuid.Nil, nil, err
	}
	jobID, err := s.RegisterDocumentJob(ctx, executor, householdID, document.ID, purpose)
	if err != nil {
		return gofrsuuid.Nil, nil, err
	}
	return document.ID, &jobID, nil
}

func (s *Service) RegisterDocumentJob(ctx context.Context, executor bob.Executor, householdID, documentID gofrsuuid.UUID, purpose string) (gofrsuuid.UUID, error) {
	if purpose != ingestion.JobPurposeInteractive && purpose != ingestion.JobPurposeBackground {
		return gofrsuuid.Nil, errors.New("invalid extraction purpose")
	}
	jobID, err := gofrsuuid.FromString(uuid.NewV7().String())
	if err != nil {
		return gofrsuuid.Nil, err
	}
	request, err := stdjson.Marshal(struct {
		Purpose string `json:"purpose"`
	}{Purpose: purpose})
	if err != nil {
		return gofrsuuid.Nil, err
	}
	requestJSON := types.JSON[stdjson.RawMessage]{Val: request}
	version := int32(1)
	status := "queued"
	job, err := models.ExtractionJobs.Insert(&models.ExtractionJobSetter{
		ID: &jobID, HouseholdID: &householdID, SourceDocumentID: &documentID, Version: &version, Status: &status, Request: &requestJSON,
	}, im.OnConflict("source_document_id", "version").DoNothing()).One(ctx, executor)
	if errors.Is(err, sql.ErrNoRows) {
		job, err = models.ExtractionJobs.Query(sm.Where(psql.And(
			models.ExtractionJobs.Columns.SourceDocumentID.EQ(psql.Arg(documentID)),
			models.ExtractionJobs.Columns.Version.EQ(psql.Arg(version)),
		))).One(ctx, executor)
	}
	if err != nil {
		return gofrsuuid.Nil, err
	}
	return job.ID, nil
}
