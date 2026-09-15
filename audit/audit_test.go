package audit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"log/slog"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	"gorbital.dev/requestid"
)

func TestEventValidate(t *testing.T) {
	tests := []struct {
		name    string
		event   audit.Event
		wantErr bool
	}{
		{"valid", audit.Event{Action: "auth.session.revoked", Outcome: audit.OutcomeSuccess}, false},
		{"two segments", audit.Event{Action: "project.created", Outcome: audit.OutcomeDenied}, false},
		{"single word action", audit.Event{Action: "login", Outcome: audit.OutcomeSuccess}, true},
		{"uppercase action", audit.Event{Action: "Auth.Login", Outcome: audit.OutcomeSuccess}, true},
		{"missing outcome", audit.Event{Action: "auth.login.succeeded"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.event.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate(%+v) = %v, want error %t", tt.event, err, tt.wantErr)
			}
		})
	}
}

func TestFromContext(t *testing.T) {
	ctx := actor.With(context.Background(), actor.Actor{Kind: actor.KindUser, ID: "usr_1", Label: "Ada", OrgID: "org_1"})
	ctx = requestid.With(ctx, "req_1")

	got := audit.FromContext(ctx, audit.Event{Action: "project.created", Outcome: audit.OutcomeSuccess})
	if got.ActorKind != actor.KindUser || got.ActorID != "usr_1" || got.ActorLabel != "Ada" || got.OrgID != "org_1" || got.RequestID != "req_1" {
		t.Errorf("FromContext() = %+v, want actor usr_1, org_1, req_1", got)
	}

	explicit := audit.FromContext(ctx, audit.Event{ActorKind: actor.KindSystem, ActorID: "job", OrgID: "org_2"})
	if explicit.ActorID != "job" || explicit.OrgID != "org_2" {
		t.Errorf("FromContext() overwrote explicit fields: %+v", explicit)
	}

	anon := audit.FromContext(context.Background(), audit.Event{})
	if anon.ActorKind != actor.KindAnonymous {
		t.Errorf("FromContext(empty context).ActorKind = %q, want anonymous", anon.ActorKind)
	}
}

func TestLogRecorder(t *testing.T) {
	var buf bytes.Buffer
	r := audit.NewLogRecorder(slog.New(slog.NewJSONHandler(&buf, nil)))
	ctx := requestid.With(context.Background(), "req_9")

	err := r.Record(ctx, audit.Event{
		Action:   "auth.login.failed",
		Outcome:  audit.OutcomeFailure,
		Metadata: map[string]any{"email": "ada@example.com"},
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log line %q is not JSON: %v", buf.String(), err)
	}
	if line["action"] != "auth.login.failed" || line["request_id"] != "req_9" || line["actor_kind"] != "anonymous" {
		t.Errorf("logged event = %v, want action, request_id and anonymous actor", line)
	}
	if strings.Contains(buf.String(), "ada@example.com") {
		t.Errorf("LogRecorder logged metadata containing personal data: %s", buf.String())
	}

	if err := r.Record(ctx, audit.Event{Action: "bad"}); err == nil {
		t.Error("Record(invalid event) = nil, want error")
	}
}
