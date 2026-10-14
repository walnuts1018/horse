package document

import (
	"context"
	"database/sql"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"uuid"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/im"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/stephenafamo/bob/types"
	"github.com/walnuts1018/horse/internal/ingestion"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

type RegistrationStore interface {
	Put(context.Context, string, string, []byte) (string, string, error)
	PutAnalysis(context.Context, string, string, string, []byte) (string, string, error)
}

type objectDeleter interface {
	Delete(context.Context, string) error
}

type RegisterCommand struct {
	HouseholdID       gofrsuuid.UUID
	MemberID          gofrsuuid.UUID
	MIMEType          string
	SourceKind        string
	Filename          string
	Purpose           string
	Model             string
	Data              []byte
	Attachments       []ingestion.EmailAttachment
	AnalysisData      []byte
	AnalysisMIMEType  string
	AnalysisTransform []byte
}

type Registration struct {
	Document  *models.SourceDocument
	Duplicate bool
	JobIDs    []gofrsuuid.UUID
}

type StoredEmailAttachment struct {
	Filename  string
	MIMEType  string
	ObjectKey string
	SHA256    string
}

func (s *Service) Register(ctx context.Context, store RegistrationStore, command RegisterCommand) (_ Registration, resultErr error) {
	if s.db == nil || store == nil {
		return Registration{}, errors.New("document registration is unavailable")
	}
	householdKey := command.HouseholdID.String()
	key, hash, err := store.Put(ctx, householdKey, command.MIMEType, command.Data)
	if err != nil {
		return Registration{}, err
	}
	storedKeys := []string{key}
	defer func() {
		if resultErr != nil {
			if cleanupErr := s.deleteUnreferencedObjects(context.WithoutCancel(ctx), store, storedKeys); cleanupErr != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("compensate document objects: %w", cleanupErr))
			}
		}
	}()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Registration{}, err
	}
	defer func() {
		if err := tx.Rollback(context.WithoutCancel(ctx)); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			resultErr = errors.Join(resultErr, fmt.Errorf("rollback document registration: %w", err))
		}
	}()
	executor := bobpgx.NewTx(tx, nil)
	documentID, err := gofrsuuid.FromString(uuid.NewV7().String())
	if err != nil {
		return Registration{}, err
	}
	filename := sql.Null[string]{V: command.Filename, Valid: command.Filename != ""}
	document, err := models.SourceDocuments.Insert(&models.SourceDocumentSetter{
		ID: &documentID, HouseholdID: &command.HouseholdID, Sha256: &hash, ObjectKey: &key, MimeType: &command.MIMEType,
		SourceKind: &command.SourceKind, SourceFilename: &filename, CreatedBy: &command.MemberID,
	}, im.OnConflict("household_id", "sha256").DoNothing()).One(ctx, executor)
	duplicate := errors.Is(err, sql.ErrNoRows)
	if duplicate {
		document, err = models.SourceDocuments.Query(sm.Where(psql.And(
			models.SourceDocuments.Columns.HouseholdID.EQ(psql.Arg(command.HouseholdID)),
			models.SourceDocuments.Columns.Sha256.EQ(psql.Arg(hash)),
		))).One(ctx, executor)
	}
	if err != nil {
		return Registration{}, err
	}
	var queued []gofrsuuid.UUID
	if !duplicate {
		if len(command.AnalysisData) > 0 {
			analysisKey, _, putErr := store.PutAnalysis(ctx, householdKey, document.ID.String(), command.AnalysisMIMEType, command.AnalysisData)
			if putErr != nil {
				return Registration{}, putErr
			}
			storedKeys = append(storedKeys, analysisKey)
			analysisObjectKey := sql.Null[string]{V: analysisKey, Valid: true}
			analysisMIME := sql.Null[string]{V: command.AnalysisMIMEType, Valid: true}
			transform := sql.Null[types.JSON[stdjson.RawMessage]]{V: types.JSON[stdjson.RawMessage]{Val: command.AnalysisTransform}, Valid: true}
			if err := document.Update(ctx, executor, &models.SourceDocumentSetter{
				AnalysisObjectKey: &analysisObjectKey, AnalysisMimeType: &analysisMIME, AnalysisTransform: &transform,
			}); err != nil {
				return Registration{}, err
			}
		}
		jobID, jobErr := s.RegisterDocumentJob(ctx, executor, command.HouseholdID, document.ID, command.Purpose, command.Model)
		if jobErr != nil {
			return Registration{}, jobErr
		}
		queued = append(queued, jobID)
	}
	storedAttachments := make([]StoredEmailAttachment, 0, len(command.Attachments))
	for _, attachment := range command.Attachments {
		attachmentKey, attachmentHash, putErr := store.Put(ctx, householdKey, attachment.MIMEType, attachment.Data)
		if putErr != nil {
			return Registration{}, putErr
		}
		storedKeys = append(storedKeys, attachmentKey)
		storedAttachments = append(storedAttachments, StoredEmailAttachment{
			Filename: attachment.Filename, MIMEType: attachment.MIMEType, ObjectKey: attachmentKey, SHA256: attachmentHash,
		})
	}
	if len(storedAttachments) > 0 {
		attachmentJobs, registerErr := s.registerStoredAttachments(ctx, executor, command.HouseholdID, command.MemberID, document.ID, command.Purpose, command.Model, storedAttachments)
		if registerErr != nil {
			return Registration{}, registerErr
		}
		queued = append(queued, attachmentJobs...)
	}
	if err := tx.Commit(ctx); err != nil {
		return Registration{}, err
	}
	return Registration{Document: document, Duplicate: duplicate, JobIDs: queued}, nil
}

func (s *Service) deleteUnreferencedObjects(ctx context.Context, store RegistrationStore, keys []string) error {
	deleter, ok := store.(objectDeleter)
	if !ok {
		return nil
	}
	var deleteErrors []error
	for _, key := range keys {
		referenced, err := models.SourceDocuments.Query(sm.Where(psql.Or(
			models.SourceDocuments.Columns.ObjectKey.EQ(psql.Arg(key)),
			models.SourceDocuments.Columns.AnalysisObjectKey.EQ(psql.Arg(sql.Null[string]{V: key, Valid: true})),
		))).Exists(ctx, bobpgx.NewPool(s.db))
		if err != nil {
			deleteErrors = append(deleteErrors, err)
			continue
		}
		if referenced {
			continue
		}
		if err := deleter.Delete(ctx, key); err != nil {
			deleteErrors = append(deleteErrors, err)
		}
	}
	return errors.Join(deleteErrors...)
}

func (s *Service) registerStoredAttachments(ctx context.Context, executor bob.Executor, householdID, memberID, parentID gofrsuuid.UUID, purpose, model string, attachments []StoredEmailAttachment) ([]gofrsuuid.UUID, error) {
	queued := make([]gofrsuuid.UUID, 0, len(attachments))
	seen := make(map[gofrsuuid.UUID]struct{}, len(attachments))
	for index, attachment := range attachments {
		childID, jobID, err := s.registerAttachmentDocument(ctx, executor, householdID, memberID, attachment, purpose, model)
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

func (s *Service) registerAttachmentDocument(ctx context.Context, executor bob.Executor, householdID, memberID gofrsuuid.UUID, attachment StoredEmailAttachment, purpose, model string) (gofrsuuid.UUID, *gofrsuuid.UUID, error) {
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
	jobID, err := s.RegisterDocumentJob(ctx, executor, householdID, document.ID, purpose, model)
	if err != nil {
		return gofrsuuid.Nil, nil, err
	}
	return document.ID, &jobID, nil
}

func (s *Service) RegisterDocumentJob(ctx context.Context, executor bob.Executor, householdID, documentID gofrsuuid.UUID, purpose, model string) (gofrsuuid.UUID, error) {
	if purpose != ingestion.JobPurposeInteractive && purpose != ingestion.JobPurposeBackground {
		return gofrsuuid.Nil, errors.New("invalid extraction purpose")
	}
	if model == "" {
		model = ingestion.ModelSelectionFree
	}
	if model != ingestion.ModelSelectionFree && model != ingestion.ModelSelectionPro {
		return gofrsuuid.Nil, errors.New("invalid extraction model selection")
	}
	jobID, err := gofrsuuid.FromString(uuid.NewV7().String())
	if err != nil {
		return gofrsuuid.Nil, err
	}
	request, err := stdjson.Marshal(struct {
		Purpose string `json:"purpose"`
		Model   string `json:"model"`
	}{Purpose: purpose, Model: model})
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
