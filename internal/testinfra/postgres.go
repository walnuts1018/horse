package testinfra

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	postgresUser     = "horse_test"
	postgresPassword = "horse_test_local_only"
	postgresDatabase = "horse_test"
)

// StartPostgresは統合テスト用のPostgreSQLコンテナを起動し、接続URLと終了関数を返す。
func StartPostgres(ctx context.Context) (string, func(context.Context) error, error) {
	container, err := postgres.Run(ctx,
		"postgres:18",
		postgres.WithDatabase(postgresDatabase),
		postgres.WithUsername(postgresUser),
		postgres.WithPassword(postgresPassword),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("5432/tcp").WithStartupTimeout(time.Minute)),
	)
	if err != nil {
		return "", nil, fmt.Errorf("start PostgreSQL test container: %w", err)
	}

	connectionURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		if terminateErr := container.Terminate(ctx); terminateErr != nil {
			err = errors.Join(err, fmt.Errorf("terminate PostgreSQL test container: %w", terminateErr))
		}
		return "", nil, fmt.Errorf("get PostgreSQL test connection URL: %w", err)
	}

	return connectionURL, func(ctx context.Context) error {
		return container.Terminate(ctx)
	}, nil
}

// ApplyMigrationsはmiseで管理しているAtlas CLIを使って既存マイグレーションを適用する。
func ApplyMigrations(ctx context.Context, databaseURL string) error {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("resolve testinfra source path")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
	command := exec.CommandContext(ctx, "mise", "run", "db:migrate")
	command.Dir = repositoryRoot
	for _, variable := range command.Environ() {
		if !strings.HasPrefix(variable, "DATABASE_URL=") {
			command.Env = append(command.Env, variable)
		}
	}
	command.Env = append(command.Env, "DATABASE_URL="+databaseURL)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("apply Atlas migrations: %w: %s", err, output)
	}
	return nil
}
