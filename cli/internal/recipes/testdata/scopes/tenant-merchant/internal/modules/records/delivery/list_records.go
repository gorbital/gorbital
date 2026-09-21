package delivery

import (
	"context"

	"gorbital.dev/page"

	"example.com/app/internal/modules/records/domain"
	"example.com/app/internal/modules/records/usecase"
)

type listRecordsInput struct {
	MerchantID string `path:"merchantId" maxLength:"64" example:"mrc_mfrggzdfmztwq2lkmfrggzdfmy"`
	page.Params
	State string `query:"state" enum:"open,done" doc:"Only records with this state"`
}

// RecordPage is a page of records.
type RecordPage struct {
	Items      []RecordResponse `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty" doc:"Pass as cursor to get the next page; absent on the last page"`
}

type recordPageOutput struct {
	Body RecordPage
}

func (h handlers) listRecords(ctx context.Context, in *listRecordsInput) (*recordPageOutput, error) {
	res, err := h.svc.ListRecords(ctx, in.MerchantID, usecase.ListRecordsInput{
		Page:  in.Params,
		State: domain.State(in.State),
	})
	if err != nil {
		return nil, fieldErrors(err, "query")
	}
	out := &recordPageOutput{Body: RecordPage{Items: make([]RecordResponse, len(res.Items)), NextCursor: res.NextCursor}}
	for i, record := range res.Items {
		out.Body.Items[i] = toResponse(record)
	}
	return out, nil
}
