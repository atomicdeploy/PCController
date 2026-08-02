package ipcjson

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// SessionTicketPath exchanges an authenticated REST request for a one-use
	// browser WebSocket ticket. The durable credential never enters a URL.
	SessionTicketPath = "/api/v1/session/ticket"

	browserWebSocketProtocol = "pccontroller.v1"
	browserTicketPrefix      = "pccontroller.ticket."
	sessionTicketLifetime    = 15 * time.Second
	maxSessionTickets        = 256
)

type requestSecurityContextKey struct{}

type requestSecurity struct {
	Principal      string
	Origin         string
	CorrelationID  string
	Authentication string
}

type sessionTicket struct {
	Origin         string
	PeerHost       string
	Transport      string
	Principal      string
	CorrelationID  string
	Authentication string
	ExpiresAt      time.Time
}

type sessionTicketRequest struct {
	Transport string `json:"transport"`
}

type sessionTicketResponse struct {
	Ticket        string    `json:"ticket"`
	Protocol      string    `json:"protocol"`
	ExpiresAt     time.Time `json:"expires_at"`
	ExpiresInMS   int64     `json:"expires_in_ms"`
	Principal     string    `json:"principal"`
	CorrelationID string    `json:"correlation_id"`
}

func setRequestSecurity(request *http.Request, security requestSecurity) {
	if request == nil {
		return
	}
	*request = *request.WithContext(context.WithValue(
		request.Context(), requestSecurityContextKey{}, security,
	))
}

func securityFromRequest(request *http.Request) requestSecurity {
	if request == nil {
		return requestSecurity{}
	}
	security, _ := request.Context().Value(requestSecurityContextKey{}).(requestSecurity)
	return security
}

func requestHeaderCredential(request *http.Request) (string, string, bool, error) {
	if request == nil {
		return "", "", false, nil
	}
	headerToken := strings.TrimSpace(request.Header.Get("X-PCController-Token"))
	authorization := strings.TrimSpace(request.Header.Get("Authorization"))
	bearer := ""
	if authorization != "" {
		if len(authorization) <= 7 || !strings.EqualFold(authorization[:7], "Bearer ") {
			return "", "", true, errors.New("Authorization must use the Bearer scheme")
		}
		bearer = strings.TrimSpace(authorization[7:])
		if bearer == "" {
			return "", "", true, errors.New("Bearer credential is empty")
		}
	}
	if headerToken != "" && bearer != "" && !secretsEqual(headerToken, bearer) {
		return "", "", true, errors.New("conflicting authentication headers")
	}
	if bearer != "" {
		return bearer, "bearer", true, nil
	}
	if headerToken != "" {
		return headerToken, "token-header", true, nil
	}
	return "", "", false, nil
}

func secretsEqual(expected, provided string) bool {
	return len(expected) == len(provided) &&
		subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}

func (service *Service) authenticatedPrincipal(token string) (string, string, bool) {
	expected := strings.TrimSpace(service.currentAuthToken())
	provided := strings.TrimSpace(token)
	if expected == "" && provided == "" {
		return "local-operator", "local-transport", true
	}
	if expected != "" && secretsEqual(expected, provided) {
		return "controller-operator", "credential", true
	}
	delegation := strings.TrimSpace(service.HostInstanceToken)
	if delegation != "" && secretsEqual(delegation, provided) {
		principal := "host-instance"
		if identity := safeAuditIdentity(service.HostInstanceID); identity != "" {
			principal += ":" + identity
		}
		return principal, "host-instance-token", true
	}
	return "", "", false
}

func safeAuditIdentity(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 96 {
		value = value[:96]
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			strings.ContainsRune("._:-", character) {
			continue
		}
		return ""
	}
	return value
}

func requestCorrelationID(request *http.Request, prefix string) string {
	if request != nil {
		for _, header := range []string{"X-Correlation-ID", "X-Request-ID"} {
			if value := safeAuditIdentity(request.Header.Get(header)); value != "" {
				return value
			}
		}
	}
	return newSecurityID(prefix)
}

func newSecurityID(prefix string) string {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err == nil {
		return prefix + "-" + hex.EncodeToString(value)
	}
	return prefix + "-" + strings.ReplaceAll(
		time.Now().UTC().Format("20060102T150405.000000000"), ".", "",
	)
}

func canonicalBrowserOrigin(request *http.Request) (string, error) {
	if request == nil {
		return "", errors.New("request is unavailable")
	}
	raw := strings.TrimSpace(request.Header.Get("Origin"))
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.User != nil || parsed.Path != "" ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("invalid Origin header")
	}
	host := strings.ToLower(parsed.Hostname())
	if port := parsed.Port(); port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return strings.ToLower(parsed.Scheme) + "://" + host, nil
}

func requestPeerHost(request *http.Request) string {
	if request == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(request.RemoteAddr))
	if err != nil {
		return strings.ToLower(strings.Trim(strings.TrimSpace(request.RemoteAddr), "[]"))
	}
	return strings.ToLower(strings.Trim(host, "[]"))
}

func (service *Service) sessionNowUTC() time.Time {
	if service.sessionClock != nil {
		return service.sessionClock().UTC()
	}
	return time.Now().UTC()
}

func (service *Service) issueSessionTicket(
	request *http.Request,
	transport string,
) (sessionTicketResponse, error) {
	security := securityFromRequest(request)
	if security.Origin == "" {
		return sessionTicketResponse{}, errors.New("browser session tickets require an explicit allowed Origin")
	}
	transport = strings.ToLower(strings.TrimSpace(transport))
	if transport != "websocket" && transport != "socket_io" {
		return sessionTicketResponse{}, errors.New("transport must be websocket or socket_io")
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return sessionTicketResponse{}, errors.New("could not create a browser WebSocket ticket")
	}
	ticket := hex.EncodeToString(random)
	digest := sha256.Sum256([]byte(ticket))
	now := service.sessionNowUTC()
	expiresAt := now.Add(sessionTicketLifetime)
	record := sessionTicket{
		Origin: security.Origin, PeerHost: requestPeerHost(request), Transport: transport,
		Principal: security.Principal, Authentication: security.Authentication,
		CorrelationID: newSecurityID("ws"), ExpiresAt: expiresAt,
	}

	service.sessionMu.Lock()
	defer service.sessionMu.Unlock()
	if service.sessionTickets == nil {
		service.sessionTickets = make(map[[sha256.Size]byte]sessionTicket)
	}
	for key, candidate := range service.sessionTickets {
		if !candidate.ExpiresAt.After(now) {
			delete(service.sessionTickets, key)
		}
	}
	if len(service.sessionTickets) >= maxSessionTickets {
		return sessionTicketResponse{}, errors.New("too many outstanding browser session tickets")
	}
	service.sessionTickets[digest] = record
	return sessionTicketResponse{
		Ticket: ticket, Protocol: browserWebSocketProtocol,
		ExpiresAt: expiresAt, ExpiresInMS: sessionTicketLifetime.Milliseconds(),
		Principal: record.Principal, CorrelationID: record.CorrelationID,
	}, nil
}

func requestedSessionTicket(request *http.Request) (string, bool) {
	if request == nil {
		return "", false
	}
	baseProtocol := false
	ticket := ""
	for _, line := range request.Header.Values("Sec-WebSocket-Protocol") {
		for _, raw := range strings.Split(line, ",") {
			protocol := strings.TrimSpace(raw)
			switch {
			case protocol == browserWebSocketProtocol:
				baseProtocol = true
			case strings.HasPrefix(protocol, browserTicketPrefix):
				if ticket != "" {
					return "", false
				}
				ticket = strings.TrimPrefix(protocol, browserTicketPrefix)
			}
		}
	}
	if !baseProtocol || len(ticket) != 64 {
		return "", false
	}
	decoded, err := hex.DecodeString(ticket)
	return strings.ToLower(ticket), err == nil && len(decoded) == 32
}

func hasSessionTicketProtocol(request *http.Request) bool {
	return request != nil && strings.Contains(
		request.Header.Get("Sec-WebSocket-Protocol"), browserTicketPrefix,
	)
}

func (service *Service) consumeSessionTicket(
	request *http.Request,
	transport string,
) (requestSecurity, bool) {
	ticket, validSyntax := requestedSessionTicket(request)
	if !validSyntax {
		return requestSecurity{}, false
	}
	digest := sha256.Sum256([]byte(ticket))
	now := service.sessionNowUTC()
	service.sessionMu.Lock()
	record, found := service.sessionTickets[digest]
	if found {
		delete(service.sessionTickets, digest)
	}
	service.sessionMu.Unlock()
	origin, err := canonicalBrowserOrigin(request)
	if !found || err != nil || origin == "" || !record.ExpiresAt.After(now) ||
		record.Origin != origin || record.PeerHost != requestPeerHost(request) ||
		record.Transport != strings.ToLower(strings.TrimSpace(transport)) {
		return requestSecurity{}, false
	}
	return requestSecurity{
		Principal: record.Principal, Origin: record.Origin,
		CorrelationID: record.CorrelationID, Authentication: "session-ticket",
	}, true
}

func authenticateHTTPRequest(
	writer http.ResponseWriter,
	request *http.Request,
	service *Service,
) bool {
	if !httpOriginAllowed(request, service.currentAllowedOrigins()) {
		writeHTTPJSON(writer, http.StatusForbidden, map[string]string{
			"error": "request origin is not allowed",
		})
		return false
	}
	credential, mechanism, headerPresent, credentialErr := requestHeaderCredential(request)
	if credentialErr != nil {
		writeHTTPJSON(writer, http.StatusUnauthorized, map[string]string{"error": credentialErr.Error()})
		return false
	}
	base := accessFromAddress(stringAddress(request.RemoteAddr), "rest")
	if strings.TrimSpace(request.Header.Get("Origin")) == "" && base.Remote && !headerPresent {
		writeHTTPJSON(writer, http.StatusForbidden, map[string]string{
			"error": "remote requests without Origin require an authentication header",
		})
		return false
	}
	principal, authentication, authenticated := service.authenticatedPrincipal(credential)
	if !authenticated {
		writer.Header().Set("WWW-Authenticate", "Bearer")
		writeHTTPJSON(writer, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return false
	}
	if mechanism != "" {
		authentication = mechanism
	}
	origin, _ := canonicalBrowserOrigin(request)
	setRequestSecurity(request, requestSecurity{
		Principal: principal, Origin: origin, Authentication: authentication,
		CorrelationID: requestCorrelationID(request, "http"),
	})
	return true
}

func authorizeWebSocketUpgrade(
	writer http.ResponseWriter,
	request *http.Request,
	service *Service,
	transport string,
) bool {
	if request == nil || request.URL == nil {
		writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid WebSocket request"})
		return false
	}
	if request.URL.Query().Has("access_token") || request.URL.Query().Has("ticket") {
		writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{
			"error": "credentials are not accepted in WebSocket URLs",
		})
		return false
	}
	if !httpOriginAllowed(request, service.currentAllowedOrigins()) {
		writeHTTPJSON(writer, http.StatusForbidden, map[string]string{"error": "request origin is not allowed"})
		return false
	}
	credential, mechanism, headerPresent, credentialErr := requestHeaderCredential(request)
	if credentialErr != nil {
		writeHTTPJSON(writer, http.StatusUnauthorized, map[string]string{"error": credentialErr.Error()})
		return false
	}
	hasOrigin := strings.TrimSpace(request.Header.Get("Origin")) != ""
	hasTicket := hasSessionTicketProtocol(request)
	if hasTicket && headerPresent {
		writeHTTPJSON(writer, http.StatusUnauthorized, map[string]string{
			"error": "ambiguous WebSocket authentication",
		})
		return false
	}
	if hasTicket {
		if !hasOrigin {
			writeHTTPJSON(writer, http.StatusForbidden, map[string]string{
				"error": "browser session tickets require an explicit Origin",
			})
			return false
		}
		security, valid := service.consumeSessionTicket(request, transport)
		if !valid {
			writer.Header().Set("WWW-Authenticate", "WebSocketTicket")
			writeHTTPJSON(writer, http.StatusUnauthorized, map[string]string{
				"error": "a fresh browser WebSocket ticket is required",
			})
			return false
		}
		setRequestSecurity(request, security)
		return true
	}
	if hasOrigin && !headerPresent {
		writer.Header().Set("WWW-Authenticate", "WebSocketTicket")
		writeHTTPJSON(writer, http.StatusUnauthorized, map[string]string{
			"error": "browser WebSocket clients require a one-use session ticket",
		})
		return false
	}
	base := accessFromAddress(stringAddress(request.RemoteAddr), transport)
	if base.Remote && !headerPresent {
		writeHTTPJSON(writer, http.StatusForbidden, map[string]string{
			"error": "remote native WebSocket clients require an authentication header",
		})
		return false
	}
	principal, authentication, authenticated := service.authenticatedPrincipal(credential)
	if !authenticated {
		writer.Header().Set("WWW-Authenticate", "Bearer")
		writeHTTPJSON(writer, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return false
	}
	if mechanism != "" {
		authentication = mechanism
	}
	origin, _ := canonicalBrowserOrigin(request)
	setRequestSecurity(request, requestSecurity{
		Principal: principal, Origin: origin, Authentication: authentication,
		CorrelationID: requestCorrelationID(request, "ws"),
	})
	return true
}

func serveSessionTicket(writer http.ResponseWriter, request *http.Request, service *Service) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authenticateHTTPRequest(writer, request, service) {
		return
	}
	if strings.TrimSpace(request.Header.Get("Origin")) == "" {
		writeHTTPJSON(writer, http.StatusForbidden, map[string]string{
			"error": "browser session tickets require an explicit allowed Origin",
		})
		return
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(request.Header.Get("Content-Type"), ";")[0]))
	if mediaType != "application/json" {
		writeHTTPJSON(writer, http.StatusUnsupportedMediaType, map[string]string{
			"error": "session ticket request requires Content-Type: application/json",
		})
		return
	}
	var params sessionTicketRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&params); err != nil {
		writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{
			"error": "session ticket request must contain one JSON object",
		})
		return
	}
	result, err := service.issueSessionTicket(request, params.Transport)
	if err != nil {
		writeHTTPJSON(writer, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Pragma", "no-cache")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writeHTTPJSON(writer, http.StatusCreated, result)
}
