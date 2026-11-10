package hostui

import (
	"context"
	"fmt"
	"net/url"
	"pccontroller.local/controller/internal/productidentity"
	"strings"
	"sync"

	"pccontroller.local/desktop/osd"
)

type OSDRequest = osd.Request
type OSDStatus = osd.Status
type OSDAction = osd.Action

var desktopOSD struct {
	sync.Mutex
	presenter osd.Presenter
}

func osdPresenter() osd.Presenter {
	desktopOSD.Lock()
	defer desktopOSD.Unlock()
	if desktopOSD.presenter == nil {
		desktopOSD.presenter = osd.New(osd.Options{Icon: loadOSDIcon, Activate: activateOSDAction})
	}
	return desktopOSD.presenter
}

func ShowOSD(ctx context.Context, request OSDRequest) (OSDStatus, error) {
	if request.Title == "" {
		request.Title = productidentity.DefaultAppTitle()
	}
	var err error
	request, err = ValidateOSD(request)
	if err != nil {
		return OSDStatus{}, err
	}
	return osdPresenter().Show(ctx, request)
}
func ValidateOSD(r OSDRequest) (OSDRequest, error) {
	r, err := osd.Normalize(r)
	if err != nil {
		return r, err
	}
	for _, a := range r.Actions {
		if err = validateActionURI(a.URI); err != nil {
			return r, err
		}
		u, _ := url.Parse(a.URI)
		if u != nil && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) && (u.Hostname() == "" || u.User != nil) {
			return r, fmt.Errorf("OSD web actions require a host and must not embed credentials")
		}
	}
	return r, nil
}
func DismissOSD(ctx context.Context, id string) (OSDStatus, error) {
	return osdPresenter().Dismiss(ctx, id)
}
func DesktopOSDStatus() OSDStatus { return osdPresenter().Status() }
func CloseDesktopOSD() {
	desktopOSD.Lock()
	p := desktopOSD.presenter
	desktopOSD.presenter = nil
	desktopOSD.Unlock()
	if p != nil {
		p.Close()
	}
}

// OSDForNotification reuses event icons and human-activated action routes.
func OSDForNotification(n Notification, durationMS int) OSDRequest {
	r := OSDRequest{ID: n.ID, Title: n.Title, Text: n.Body, Icon: n.Icon, DurationMS: durationMS}
	if r.Text == "" {
		r.Text = n.Title
	}
	switch n.Severity {
	case "error", "warning", "success", "info":
		r.Severity = n.Severity
	}
	for _, a := range n.Actions {
		r.Actions = append(r.Actions, OSDAction{Label: a.Label, URI: a.URI})
	}
	return r
}
