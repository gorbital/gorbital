package signintest

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"gorbital.dev/httpx"
)

// ConsolePrefix is where the dev console serves the tests.
const ConsolePrefix = "/_dev/auth/test/"

// ConsoleHandler serves the tests' endpoints under ConsolePrefix (and
// /_dev/auth/test itself). The dev console checks every request before it
// gets here (ADR-0065, ADR-0086).
func (t *Tester) ConsoleHandler() http.Handler {
	mux := http.NewServeMux()
	overview := func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, t.Overview()) }
	mux.HandleFunc("GET /_dev/auth/test", overview)
	mux.HandleFunc("GET "+ConsolePrefix+"{$}", overview)
	mux.HandleFunc("POST "+ConsolePrefix+"{method}/check", func(w http.ResponseWriter, r *http.Request) {
		res, err := t.NetworkChecks(r.Context(), socialMethod(r.PathValue("method")))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("POST "+ConsolePrefix+"{method}/start", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ResultURL string `json:"result_url"`
		}
		if !decode(w, r, &body) {
			return
		}
		var (
			res Start
			err error
		)
		if method := r.PathValue("method"); method == MethodPasskeys {
			res, err = t.StartPasskey(body.ResultURL)
		} else {
			res, err = t.StartSocial(socialMethod(method), body.ResultURL)
		}
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, res)
	})
	mux.HandleFunc("POST "+ConsolePrefix+"{method}/id-token", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IDToken string `json:"id_token"`
			Nonce   string `json:"nonce"`
		}
		if !decode(w, r, &body) {
			return
		}
		res, err := t.VerifyIDToken(r.Context(), socialMethod(r.PathValue("method")), body.IDToken, body.Nonce)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("GET "+ConsolePrefix+"results/{id}", func(w http.ResponseWriter, r *http.Request) {
		res, err := t.Result(r.PathValue("id"))
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc("POST "+ConsolePrefix+"totp/start", func(w http.ResponseWriter, r *http.Request) {
		res, err := t.StartTOTP()
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, res)
	})
	mux.HandleFunc("POST "+ConsolePrefix+"totp/verify", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID   string `json:"id"`
			Code string `json:"code"`
		}
		if !decode(w, r, &body) {
			return
		}
		res, err := t.VerifyTOTP(body.ID, body.Code)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
	mux.HandleFunc(ConsolePrefix, func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusNotFound, "not_found", "no sign-in test endpoint "+r.Method+" "+r.URL.Path))
	})
	return mux
}

// socialMethod keeps only the providers' names, so a path value never
// reaches a message unchecked.
func socialMethod(method string) string {
	switch method {
	case MethodGoogle, MethodApple, MethodGitHub:
		return method
	}
	return "unknown"
}

// decode reads a JSON body of at most 64 KiB; an empty body is {}.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	data, err := io.ReadAll(io.LimitReader(r.Body, 64<<10+1))
	if err == nil && len(data) > 64<<10 {
		err = errors.New("too large")
	}
	if err == nil && len(data) > 0 {
		err = json.Unmarshal(data, v)
	}
	if err != nil {
		httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusBadRequest, "invalid_json", "the body must be a JSON object of at most 64 KiB"))
		return false
	}
	return true
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	var unavailable *UnavailableError
	switch {
	case errors.As(err, &unavailable):
		p := httpx.NewProblem(http.StatusConflict, "live_test_unavailable", unavailable.Reason)
		httpx.WriteProblem(w, r, p)
	case errors.Is(err, ErrNotConfigured):
		httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusNotFound, "not_configured", err.Error()))
	case errors.Is(err, ErrTestNotFound):
		httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusNotFound, "test_not_found", err.Error()))
	case errors.Is(err, ErrInvalidResultURL):
		httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusUnprocessableEntity, "invalid_result_url", err.Error()))
	case errors.Is(err, ErrInvalidRequest):
		httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusUnprocessableEntity, "invalid_request", err.Error()))
	case errors.Is(err, ErrTooManyTests):
		w.Header().Set("Retry-After", "60")
		httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusTooManyRequests, "too_many_tests", err.Error()))
	default:
		httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusInternalServerError, "internal_error", "the sign-in test failed; see the app's logs"))
	}
}
