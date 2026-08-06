package ipcjson

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// SessionTicketPath exchanges a header credential for a one-use browser
	// WebSocket credential. Durable credentials are never put in a URL.
	SessionTicketPath = "/api/session/ticket"

	browserWebSocketProtocol = "pccontroller"
	browserTicketPrefix      = "pccontroller.ticket."
	sessionTicketLifetime    = 30 * time.Second
	maxSessionTickets        = 256
)

type authenticatedAccessKey struct{}

type sessionTicket struct {
	Access    Access
	Transport string
	Origin    string
	PeerHost  string
	ExpiresAt time.Time
}

type sessionTicketRequest struct {
	Transport string `json:"transport"`
}

type sessionTicketResponse struct {
	Ticket      string    `json:"ticket"`
	Protocol    string    `json:"protocol"`
	ExpiresAt   time.Time `json:"expires_at"`
	ExpiresInMS int64     `json:"expires_in_ms"`
	Principal   string    `json:"principal"`
}

// normalizeAccess supplies stable audit identities even for trusted in-process
// and local IPC callers that do not perform an HTTP authentication exchange.
func (service *Service) normalizeAccess(access Access) Access {
	access.Transport = strings.ToLower(strings.TrimSpace(access.Transport))
	if access.Transport == "" {
		access.Transport = "ipc"
	}
	access.Principal = strings.TrimSpace(access.Principal)
	if access.Principal == "" {
		switch {
		case access.Transport == "bridge":
			access.Principal = "bridge-peer"
		case access.Remote:
			access.Principal = service.currentRemotePrincipal()
		default:
			access.Principal = "local-operator"
		}
	}
	access.Authentication = strings.ToLower(strings.TrimSpace(access.Authentication))
	if access.Authentication == "" {
		if access.authenticated {
			access.Authentication = "delegated"
		} else {
			access.Authentication = "pending"
		}
	}
	return access
}

func (service *Service) currentRemotePrincipal() string {
	principal := strings.TrimSpace(service.RemotePrincipal)
	if service.HostConfig != nil {
		principal = strings.TrimSpace(service.HostConfig().IPC.RemotePrincipal)
	}
	if principal == "" {
		return "remote-operator"
	}
	return principal
}

func (service *Service) authenticateAccess(access Access, token, mechanism string) (Access, bool) {
	access = service.normalizeAccess(access)
	provided := strings.TrimSpace(token)
	expected := strings.TrimSpace(service.currentAuthToken())
	delegation := strings.TrimSpace(service.HostInstanceToken)

	if expected == "" && provided == "" && !access.Remote {
		access.authenticated = true
		access.Principal = "local-operator"
		access.Authentication = "local-transport"
		return access, true
	}
	if expected != "" && secretsEqual(expected, provided) {
		access.authenticated = true
		access.Principal = service.currentRemotePrincipal()
		access.Authentication = mechanism
		return access, true
	}
	if delegation != "" && secretsEqual(delegation, provided) {
		access.authenticated = true
		access.Principal = "host-instance"
		if identity := strings.TrimSpace(service.HostInstanceID); identity != "" {
			access.Principal += ":" + identity
		}
		access.Authentication = "host-instance-token"
		return access, true
	}
	return access, false
}

func secretsEqual(expected, provided string) bool {
	return len(expected) == len(provided) &&
		subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}

func (service *Service) sessionNow() time.Time {
	if service.sessionClock != nil {
		return service.sessionClock().UTC()
	}
	return time.Now().UTC()
}

func (service *Service) issueSessionTicket(
	access Access,
	request *http.Request,
	transport string,
) (sessionTicketResponse, error) {
	access = service.normalizeAccess(access)
	transport = strings.ToLower(strings.TrimSpace(transport))
	if transport != "websocket" && transport != "socket_io" {
		return sessionTicketResponse{}, errors.New("transport must be websocket or socket_io")
	}
	origin, err := canonicalRequestOrigin(request)
	if err != nil || origin == "" {
		return sessionTicketResponse{}, errors.New("browser session tickets require an allowed Origin header")
	}
	if access.Remote && !service.hostConfig().IPC.AllowRemote {
		service.auditAccess(access, "POST "+SessionTicketPath, "session", false)
		return sessionTicketResponse{}, errors.New("remote network access is disabled")
	}

	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return sessionTicketResponse{}, fmt.Errorf("create session ticket: %w", err)
	}
	ticket := hex.EncodeToString(secret)
	now := service.sessionNow()
	expiresAt := now.Add(sessionTicketLifetime)
	record := sessionTicket{
		Access: access, Transport: transport, Origin: origin,
		PeerHost: peerHost(request.RemoteAddr), ExpiresAt: expiresAt,
	}

	service.sessionMu.Lock()
	if service.sessionTickets == nil {
		service.sessionTickets = make(map[string]sessionTicket)
	}
	for key, candidate := range service.sessionTickets {
		if !candidate.ExpiresAt.After(now) {
			delete(service.sessionTickets, key)
		}
	}
	if len(service.sessionTickets) >= maxSessionTickets {
		var oldestKey string
		var oldest time.Time
		for key, candidate := range service.sessionTickets {
			if oldestKey == "" || candidate.ExpiresAt.Before(oldest) {
				oldestKey, oldest = key, candidate.ExpiresAt
			}
		}
		delete(service.sessionTickets, oldestKey)
	}
	service.sessionTickets[ticket] = record
	service.sessionMu.Unlock()

	service.auditAccess(access, "POST "+SessionTicketPath, "session", true)
	return sessionTicketResponse{
		Ticket: ticket, Protocol: browserWebSocketProtocol,
		ExpiresAt: expiresAt, ExpiresInMS: sessionTicketLifetime.Milliseconds(),
		Principal: access.Principal,
	}, nil
}

func (service *Service) consumeSessionTicket(
	request *http.Request,
	base Access,
	transport string,
) (Access, bool) {
	ticket, protocolPresent, ok := requestedSessionTicket(request)
	if !ok || !protocolPresent {
		return base, false
	}
	now := service.sessionNow()
	service.sessionMu.Lock()
	record, exists := service.sessionTickets[ticket]
	if exists {
		delete(service.sessionTickets, ticket)
	}
	service.sessionMu.Unlock()
	if !exists || !record.ExpiresAt.After(now) ||
		record.Transport != strings.ToLower(strings.TrimSpace(transport)) {
		return base, false
	}
	origin, err := canonicalRequestOrigin(request)
	if err != nil || origin == "" || origin != record.Origin ||
		peerHost(request.RemoteAddr) != record.PeerHost || base.Remote != record.Access.Remote {
		return base, false
	}
	access := record.Access
	access.Transport = strings.ToLower(strings.TrimSpace(transport))
	access.Authentication = "session-ticket"
	access.authenticated = true
	return service.normalizeAccess(access), true
}

func requestedSessionTicket(request *http.Request) (ticket string, protocolPresent, ok bool) {
	if request == nil {
		return "", false, false
	}
	for _, line := range request.Header.Values("Sec-WebSocket-Protocol") {
		for _, raw := range strings.Split(line, ",") {
			protocol := strings.TrimSpace(raw)
			if protocol == browserWebSocketProtocol {
				protocolPresent = true
				continue
			}
			if !strings.HasPrefix(protocol, browserTicketPrefix) {
				continue
			}
			if ticket != "" {
				return "", protocolPresent, false
			}
			ticket = strings.TrimPrefix(protocol, browserTicketPrefix)
		}
	}
	if len(ticket) != 64 {
		return "", protocolPresent, false
	}
	decoded, err := hex.DecodeString(ticket)
	return strings.ToLower(ticket), protocolPresent, err == nil && len(decoded) == 32
}

func canonicalRequestOrigin(request *http.Request) (string, error) {
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
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host), nil
}

func peerHost(address string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return strings.ToLower(strings.Trim(strings.TrimSpace(address), "[]"))
	}
	return strings.ToLower(strings.Trim(host, "[]"))
}

func headerCredential(request *http.Request) (token, mechanism string, present bool, err error) {
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

func websocketTransport(request *http.Request, service *Service) string {
	if request == nil || request.URL == nil || !strings.EqualFold(request.Header.Get("Upgrade"), "websocket") {
		return ""
	}
	path := request.URL.Path
	if path == service.currentSocketIOPath() {
		return "socket_io"
	}
	if path == service.currentWebSocketPath() {
		return "websocket"
	}
	return ""
}

func (service *Service) currentWebSocketPath() string {
	path := strings.TrimSpace(service.WebSocketPath)
	if path == "" && service.HostConfig != nil {
		path = strings.TrimSpace(service.HostConfig().IPC.WebSocketPath)
	}
	if path == "" {
		return "/ipc"
	}
	return path
}

func (service *Service) currentSocketIOPath() string {
	path := strings.TrimSpace(service.SocketIOPath)
	if path == "" && service.HostConfig != nil {
		path = strings.TrimSpace(service.HostConfig().IPC.SocketIOPath)
	}
	if path == "" {
		return "/socket.io/"
	}
	return path
}

func (service *Service) authorizeHTTPRequest(writer http.ResponseWriter, request *http.Request) bool {
	base := accessFromAddress(nil, "rest")
	if request != nil {
		base = accessFromAddress(stringAddress(request.RemoteAddr), "rest")
	}
	base = service.normalizeAccess(base)

	credential, mechanism, headerPresent, credentialErr := headerCredential(request)
	if request != nil && request.URL != nil && request.URL.Query().Has("access_token") {
		writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{
			"error": "URL credentials are not accepted; use a header or one-time WebSocket ticket",
		})
		return false
	}
	if credentialErr != nil {
		service.auditAccess(Access{Remote: base.Remote, Transport: base.Transport, Principal: "unauthenticated", Authentication: mechanism}, request.Method+" "+request.URL.Path, "authentication", false)
		writeHTTPJSON(writer, http.StatusUnauthorized, map[string]string{"error": credentialErr.Error()})
		return false
	}
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" && base.Remote && !headerPresent {
		writeHTTPJSON(writer, http.StatusForbidden, map[string]string{
			"error": "remote requests without Origin require an Authorization or X-PCController-Token header",
		})
		return false
	}
	if !httpOriginAllowed(request, service.currentAllowedOrigins()) {
		writeHTTPJSON(writer, http.StatusForbidden, map[string]string{"error": "request origin is not allowed"})
		return false
	}

	transport := websocketTransport(request, service)
	ticket, _, ticketSyntaxOK := requestedSessionTicket(request)
	hasTicket := ticket != "" || strings.Contains(request.Header.Get("Sec-WebSocket-Protocol"), browserTicketPrefix)
	if hasTicket && (transport == "" || headerPresent || !ticketSyntaxOK) {
		writeHTTPJSON(writer, http.StatusUnauthorized, map[string]string{"error": "invalid or ambiguous WebSocket session ticket"})
		return false
	}
	var access Access
	var authenticated bool
	if hasTicket {
		access, authenticated = service.consumeSessionTicket(request, base, transport)
	} else {
		access, authenticated = service.authenticateAccess(base, credential, mechanism)
	}
	if !authenticated {
		service.auditAccess(Access{Remote: base.Remote, Transport: firstNonempty(transport, "rest"), Principal: "unauthenticated", Authentication: firstNonempty(mechanism, "none")}, request.Method+" "+request.URL.Path, "authentication", false)
		writer.Header().Set("WWW-Authenticate", "Bearer")
		writeHTTPJSON(writer, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return false
	}
	if transport != "" {
		access.Transport = transport
	}
	access = service.normalizeAccess(access)
	requestWithAccess := request.WithContext(context.WithValue(request.Context(), authenticatedAccessKey{}, access))
	*request = *requestWithAccess
	writer.Header().Set("X-PCController-Principal", access.Principal)
	writer.Header().Set("X-PCController-Authentication", access.Authentication)
	return true
}

func authenticatedHTTPRequestAccess(request *http.Request, transport string) Access {
	base := accessFromAddress(nil, transport)
	if request != nil {
		base = accessFromAddress(stringAddress(request.RemoteAddr), transport)
		if access, ok := request.Context().Value(authenticatedAccessKey{}).(Access); ok {
			access.Remote = base.Remote
			access.Transport = transport
			return access
		}
	}
	return base
}

func serveSessionTicket(writer http.ResponseWriter, request *http.Request, service *Service) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorizeHTTPRequest(writer, request, service) {
		return
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(request.Header.Get("Content-Type"), ";")[0]))
	if mediaType != "application/json" {
		writeHTTPJSON(writer, http.StatusUnsupportedMediaType, map[string]string{"error": "session ticket request requires Content-Type: application/json"})
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
		writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": "session ticket request must contain one JSON object"})
		return
	}
	params.Transport = strings.ToLower(strings.TrimSpace(params.Transport))
	if params.Transport != "websocket" && params.Transport != "socket_io" {
		writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{
			"error": "transport must be websocket or socket_io",
		})
		return
	}
	access := authenticatedHTTPRequestAccess(request, "rest")
	result, err := service.issueSessionTicket(access, request, params.Transport)
	if err != nil {
		writeHTTPJSON(writer, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Pragma", "no-cache")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writeHTTPJSON(writer, http.StatusCreated, result)
}
