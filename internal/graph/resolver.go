package graph

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/walnuts1018/horse/internal/auth"
)

type Resolver struct {
	DB   *pgxpool.Pool
	Auth *auth.Service
}
