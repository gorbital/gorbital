package main

import (
	"net/http"
	"strings"
	"testing"

	"gorbital.dev/gorbital/gorbitaltest"
)

// register creates a reader's account with a display name, verifies it and
// signs in.
func register(t *testing.T, app *gorbitaltest.App, email, displayName string) *gorbitaltest.Client {
	t.Helper()
	app.Client().Post("/v1/auth/register", map[string]string{"email": email, "password": readerPassword, "display_name": displayName}).
		AssertStatus(t, http.StatusAccepted)
	return verifyAndSignIn(t, app, email)
}

// invitationToken returns the token of the last invitation emailed to
// email, from the link's #token= fragment.
func invitationToken(t *testing.T, app *gorbitaltest.App, email string) string {
	t.Helper()
	token := ""
	for _, m := range app.Mail(t) {
		if len(m.To) == 0 || m.To[0].Email != email {
			continue
		}
		if _, after, ok := strings.Cut(m.Text, "#token="); ok {
			token, _, _ = strings.Cut(after, "\n")
			token = strings.TrimSpace(token)
		}
	}
	if token == "" {
		t.Fatalf("no invitation was emailed to %s", email)
	}
	return token
}

// docs:start book-club

// TestBookClub: a book club is an organisation. Ada starts one and invites
// Bob, both keep its reading list, and for Carol, who isn't a member, the
// club doesn't exist.
func TestBookClub(t *testing.T) {
	app := newAccountsApp(t)
	ada := register(t, app, "ada@example.com", "Ada")
	bob := register(t, app, "bob@example.com", "Bob")
	carol := register(t, app, "carol@example.com", "Carol")

	res := ada.Post("/v1/orgs", map[string]string{"name": "Thursday Book Club"})
	res.AssertStatus(t, http.StatusCreated)
	var club struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	res.JSON(t, &club)
	if club.Role != "owner" {
		t.Errorf("Ada's role in her club = %q, want owner", club.Role)
	}
	readingList := "/v1/orgs/" + club.ID + "/club-books"

	// Bob joins through the link in his invitation email.
	ada.Post("/v1/orgs/"+club.ID+"/invitations", map[string]string{"email": "bob@example.com", "role": "member"}).
		AssertStatus(t, http.StatusCreated)
	bob.Get(readingList).AssertProblem(t, http.StatusNotFound, "org_not_found") // not a member yet
	bob.Post("/v1/invitations/accept", map[string]string{"token": invitationToken(t, app, "bob@example.com")}).
		AssertStatus(t, http.StatusOK)

	// Members of every role keep the reading list together.
	bob.Post(readingList, map[string]string{"title": "Middlemarch", "author": "George Eliot"}).AssertStatus(t, http.StatusCreated)
	ada.Post(readingList, map[string]string{"title": "middlemarch"}).AssertProblem(t, http.StatusConflict, "club_book_title_taken")
	var list struct {
		Items []struct {
			Title  string `json:"title"`
			Status string `json:"status"`
		} `json:"items"`
	}
	ada.Get(readingList).JSON(t, &list)
	if len(list.Items) != 1 || list.Items[0].Title != "Middlemarch" || list.Items[0].Status != "proposed" {
		t.Errorf("the club's reading list = %+v, want Bob's proposal", list.Items)
	}

	// Carol isn't a member: every route answers as for an unknown club.
	carol.Get(readingList).AssertProblem(t, http.StatusNotFound, "org_not_found")
	carol.Post(readingList, map[string]string{"title": "Emma"}).AssertProblem(t, http.StatusNotFound, "org_not_found")
}

// docs:end book-club
