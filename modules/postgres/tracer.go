package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "gorbital.dev/modules/postgres"

// tracer turns each query into a client span following the OpenTelemetry
// database semantic conventions. Query arguments are never recorded.
type tracer struct {
	tracer trace.Tracer
}

func newTracer(tp trace.TracerProvider) *tracer {
	return &tracer{tracer: tp.Tracer(instrumentationName)}
}

func (t *tracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	attrs := []attribute.KeyValue{
		attribute.String("db.system.name", "postgresql"),
		attribute.String("db.query.text", data.SQL),
	}
	if conn != nil {
		if db := conn.Config().Database; db != "" {
			attrs = append(attrs, attribute.String("db.namespace", db))
		}
	}
	ctx, _ = t.tracer.Start(ctx, operationName(data.SQL),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)
	return ctx
}

func (t *tracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span := trace.SpanFromContext(ctx)
	defer span.End()
	if data.Err == nil || errors.Is(data.Err, pgx.ErrNoRows) {
		return
	}
	var pgErr *pgconn.PgError
	if errors.As(data.Err, &pgErr) {
		span.SetAttributes(attribute.String("db.response.status_code", pgErr.Code))
	}
	// PgError.Error() holds the message and SQLSTATE, not the row values in
	// its Detail field.
	span.RecordError(data.Err)
	span.SetStatus(codes.Error, "query failed")
}

// operationName returns the SQL command (SELECT, INSERT, ...) for the span
// name, or "postgresql" when the statement doesn't start with a keyword.
func operationName(sql string) string {
	fields := strings.Fields(sql)
	if len(fields) == 0 {
		return "postgresql"
	}
	op := strings.ToUpper(fields[0])
	for _, r := range op {
		if r < 'A' || r > 'Z' {
			return "postgresql"
		}
	}
	return op
}
