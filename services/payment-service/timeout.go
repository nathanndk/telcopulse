package payment

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// simulateDatabaseTimeout confines statement cancellation to a savepoint so the
// outer transaction can persist the observed failure and its outbox atomically.
func simulateDatabaseTimeout(ctx context.Context, tx pgx.Tx, timeoutMS int) error {
	if timeoutMS < 100 || timeoutMS > 4500 {
		return errors.New("invalid simulation timeout")
	}
	nested, err := tx.Begin(ctx)
	if err != nil {
		return err
	}
	if _, err = nested.Exec(ctx, "SELECT set_config('statement_timeout',$1,true)", fmt.Sprintf("%dms", timeoutMS)); err != nil {
		return err
	}
	_, err = nested.Exec(ctx, "SELECT pg_sleep($1)", float64(timeoutMS+100)/1000)
	var pgerr *pgconn.PgError
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !errors.As(err, &pgerr) || pgerr.Code != "57014" || pgerr.Message != "canceling statement due to statement timeout" {
		if err == nil {
			return errors.New("expected database timeout did not occur")
		}
		return err
	}
	// ROLLBACK TO SAVEPOINT also restores the prior statement_timeout setting.
	return nested.Rollback(ctx)
}
