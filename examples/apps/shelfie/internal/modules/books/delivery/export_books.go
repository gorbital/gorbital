package delivery

import "context"

type exportOutput struct {
	Body struct {
		// ExportedBy names the app that asked, so a reader sending the file
		// back with a complaint says which build wrote it. Empty when the
		// caller sent no X-App-Version.
		ExportedBy string         `json:"exported_by,omitempty" example:"shelfie/2.4.0"`
		Items      []BookResponse `json:"items"`
	}
}

// docs:start export-books

func (h handlers) exportBooks(ctx context.Context, _ *struct{}) (*exportOutput, error) {
	books, err := h.svc.ExportBooks(ctx)
	if err != nil {
		return nil, err
	}
	out := &exportOutput{}
	// What RequireClientVersion put in the context reaches the handler: the
	// Huma context carries the request's.
	if version, ok := ClientVersionFrom(ctx); ok {
		out.Body.ExportedBy = "shelfie/" + version.String()
	}
	out.Body.Items = make([]BookResponse, 0, len(books))
	for _, b := range books {
		out.Body.Items = append(out.Body.Items, toResponse(b))
	}
	return out, nil
}

// docs:end export-books
