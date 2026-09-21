package delivery

import (
	"context"

	"example.com/app/internal/modules/records/domain"
)

type createRecordInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body  struct {
		Title string `json:"title" minLength:"1" maxLength:"100" doc:"Unique in the organisation, ignoring case"`
		Note  string `json:"note,omitempty" maxLength:"2000"`
		State string `json:"state,omitempty" enum:"open,done" default:"open"`
	}
}

func (h handlers) createRecord(ctx context.Context, in *createRecordInput) (*recordOutput, error) {
	record, err := h.svc.CreateRecord(ctx, in.OrgID, domain.RecordFields{
		Title: in.Body.Title,
		Note:  in.Body.Note,
		State: domain.State(in.Body.State),
	})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &recordOutput{Body: toResponse(record)}, nil
}
