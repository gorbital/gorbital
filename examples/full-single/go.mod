module example.com/acme-api

go 1.26.0

require (
	apistock.dev v0.0.0
	apistock.dev/modules/auditpg v0.0.0
	apistock.dev/modules/auth v0.0.0
	apistock.dev/modules/jobs v0.0.0
	apistock.dev/modules/mail/resend v0.0.0
	apistock.dev/modules/mail/smtp v0.0.0
	apistock.dev/modules/openapi v0.0.0
	apistock.dev/modules/postgres v0.0.0
	apistock.dev/modules/settings v0.0.0
	apistock.dev/modules/telemetry v0.0.0
	github.com/danielgtaylor/huma/v2 v2.39.1
	github.com/jackc/pgx/v5 v5.11.0
	github.com/riverqueue/river v0.47.0
	github.com/riverqueue/river/rivertype v0.47.0
)

require (
	github.com/coreos/go-oidc/v3 v3.21.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.3 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/go-webauthn/webauthn v0.18.1 // indirect
	github.com/go-webauthn/x v0.3.1 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	golang.org/x/oauth2 v0.37.0 // indirect
	rsc.io/qr v0.2.0 // indirect
)

require (
	apistock.dev/modules/releases v0.0.0
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.30.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/pressly/goose/v3 v3.28.0 // indirect
	github.com/riverqueue/river/riverdriver v0.47.0 // indirect
	github.com/riverqueue/river/riverdriver/riverpgxv5 v0.47.0 // indirect
	github.com/riverqueue/river/rivershared v0.47.0 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	github.com/sethvargo/go-retry v0.4.0 // indirect
	github.com/tidwall/gjson v1.19.0 // indirect
	github.com/tidwall/match v1.2.0 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.71.0 // indirect
	go.opentelemetry.io/otel v1.46.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp v1.46.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.46.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.46.0 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/sdk v1.46.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/trace v1.46.0 // indirect
	go.opentelemetry.io/proto/otlp v1.11.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	golang.org/x/time v0.16.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260819154853-08b0e4226688 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260831171406-18b4a7587f8a // indirect
	google.golang.org/grpc v1.83.2 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

replace (
	apistock.dev => ../..
	apistock.dev/modules/auditpg => ../../modules/auditpg
	apistock.dev/modules/auth => ../../modules/auth
	apistock.dev/modules/jobs => ../../modules/jobs
	apistock.dev/modules/mail/resend => ../../modules/mail/resend
	apistock.dev/modules/mail/smtp => ../../modules/mail/smtp
	apistock.dev/modules/openapi => ../../modules/openapi
	apistock.dev/modules/postgres => ../../modules/postgres
	apistock.dev/modules/settings => ../../modules/settings
	apistock.dev/modules/telemetry => ../../modules/telemetry
)

replace apistock.dev/modules/releases => ../../modules/releases
