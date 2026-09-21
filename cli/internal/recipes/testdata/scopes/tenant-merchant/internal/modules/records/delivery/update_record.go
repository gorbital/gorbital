package delivery

import (
	"context"

	"example.com/app/internal/modules/records/domain"
	"example.com/app/internal/modules/records/usecase"
)

type updateRecordInput struct {
	MerchantID string `path:"merchantId" maxLength:"64" example:"mrc_mfrggzdfmztwq2lkmfrggzdfmy"`
	ID         string `path:"id" maxLength:"64" example:"rcr_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body       struct {
		Version int64   `json:"version" minimum:"1" doc:"The version you read. If the record changed since, the update fails with record_version_conflict."`
		Title   *string `json:"title,omitempty" minLength:"1" maxLength:"100"`
		Note    *string `json:"note,omitempty" maxLength:"2000"`
		State   *string `json:"state,omitempty" enum:"open,done"`
	}
}

func (h handlers) updateRecord(ctx context.Context, in *updateRecordInput) (*recordOutput, error) {
	changes := domain.Changes{
		Title: in.Body.Title,
		Note:  in.Body.Note,
	}
	if in.Body.State != nil {
		value := domain.State(*in.Body.State)
		changes.State = &value
	}
	record, err := h.svc.UpdateRecord(ctx, in.MerchantID, in.ID, usecase.UpdateRecordInput{Version: in.Body.Version, Changes: changes})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &recordOutput{Body: toResponse(record)}, nil
}
