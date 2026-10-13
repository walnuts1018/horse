package graph

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stephenafamo/bob"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
)

func queryExecutor(db *pgxpool.Pool) bob.Executor { return bobpgx.NewPool(db) }
