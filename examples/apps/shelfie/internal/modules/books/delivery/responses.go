package delivery

import (
	"time"

	"example.com/shelfie/internal/modules/books/domain"
)

// BookResponse is a book as the API returns it.
type BookResponse struct {
	ID        string    `json:"id" example:"bok_mfrggzdfmztwq2lkmfrggzdfmy"`
	Title     string    `json:"title" example:"Dune"`
	Author    string    `json:"author" example:"Frank Herbert"`
	ISBN      string    `json:"isbn,omitempty" example:"9780441172719"`
	Status    string    `json:"status" enum:"want_to_read,reading,read"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type bookOutput struct {
	Body BookResponse
}

// bookIDInput is the path of the routes on one book.
type bookIDInput struct {
	ID string `path:"id" maxLength:"64" example:"bok_mfrggzdfmztwq2lkmfrggzdfmy"`
}

func toResponse(b domain.Book) BookResponse {
	return BookResponse{
		ID: b.ID, Title: b.Title, Author: b.Author, ISBN: b.ISBN, Status: string(b.Status),
		CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt,
	}
}
