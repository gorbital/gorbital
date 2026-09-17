package delivery

import "context"

type announcementListOutput struct {
	Body struct {
		Items []AnnouncementResponse `json:"items"`
	}
}

func (h handlers) listAnnouncements(ctx context.Context, _ *struct{}) (*announcementListOutput, error) {
	list, err := h.svc.ListAnnouncements(ctx)
	if err != nil {
		return nil, err
	}
	out := &announcementListOutput{}
	out.Body.Items = make([]AnnouncementResponse, 0, len(list))
	for _, a := range list {
		out.Body.Items = append(out.Body.Items, toResponse(a))
	}
	return out, nil
}
