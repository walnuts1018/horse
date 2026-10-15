package ingestion

import (
	"context"
	"fmt"
	"time"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/stephenafamo/bob/dialect/psql"
	"github.com/stephenafamo/bob/dialect/psql/um"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/postgres/generated/models"
)

func (w *Worker) saveInteractionReplyPreview(ctx context.Context, j job, preview string) error {
	jobID, err := gofrsuuid.FromString(j.ID)
	if err != nil {
		return fmt.Errorf("parse interaction job ID: %w", err)
	}
	attempt, err := postgresInt32(j.Attempt, "extraction attempt")
	if err != nil {
		return err
	}
	updatedAt := time.Now().UTC()
	updated, err := models.ExtractionJobs.Update((&models.ExtractionJobSetter{
		InteractionReplyPreview: &preview, UpdatedAt: &updatedAt,
	}).UpdateMod(), um.Where(psql.And(
		models.ExtractionJobs.Columns.ID.EQ(psql.Arg(jobID)),
		models.ExtractionJobs.Columns.Status.EQ(psql.Arg("running")),
		models.ExtractionJobs.Columns.Attempt.EQ(psql.Arg(attempt)),
	))).Exec(ctx, bobpgx.NewPool(w.DB))
	if err != nil {
		return fmt.Errorf("save interaction reply preview: %w", err)
	}
	if updated == 0 {
		return fmt.Errorf("interaction job is no longer running")
	}
	return nil
}
