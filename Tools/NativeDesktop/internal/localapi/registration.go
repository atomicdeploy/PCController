package localapi

import (
	"bytes"
	"encoding/json"
	"net/http"

	"rayanlamp.local/app/internal/commands"
)

type integrationRegistrationRequest struct {
	Method string `json:"method"`
}

var integrationRegistrationMutations = map[string]struct{}{
	commands.MethodIntegrationShellRegister:   {},
	commands.MethodIntegrationShellUnregister: {},
	commands.MethodIntegrationCOMRegister:     {},
	commands.MethodIntegrationCOMUnregister:   {},
}

// integrationRegistration exposes only the deliberately narrow, opt-in
// registration surface. Reads never mutate registration state.
func (a *api) integrationRegistration(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if err := requireEmptyBody(r); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		a.execute(w, r, commands.MethodIntegrationRegistrationStatus, nil)
	case http.MethodPost:
		if !requireJSONContentType(w, r) {
			return
		}
		raw, err := readJSONBody(w, r)
		if err != nil {
			writeBodyError(w, err)
			return
		}
		var request integrationRegistrationRequest
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			writeError(
				w,
				http.StatusBadRequest,
				"invalid_json",
				"body must contain only a supported method string",
			)
			return
		}
		if _, ok := integrationRegistrationMutations[request.Method]; !ok {
			writeError(
				w,
				http.StatusBadRequest,
				"invalid_argument",
				"method is not a supported registration action",
			)
			return
		}
		a.execute(w, r, request.Method, nil)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
	}
}
