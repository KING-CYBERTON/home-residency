package client

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
)

// ---- complaints

func (p *Portal) complaintsPage(w http.ResponseWriter, r *http.Request) {
	var page gen.ComplaintPage
	if err := p.call(r, nil, &page, func(w http.ResponseWriter, r *http.Request) {
		p.API.GetClientComplaints(w, r, gen.GetClientComplaintsParams{})
	}); err != nil {
		p.failPage(w, r, err)
		return
	}
	p.render(w, r, "complaints", &view{Title: "Complaints", Nav: "account", Data: page})
}

type complaintForm struct {
	Requests            []gen.ServiceRequest
	RequestID, Severity string
	Summary, Details    string
}

func (p *Portal) complaintForm(w http.ResponseWriter, r *http.Request, f *complaintForm, errMsg string) {
	var page requestPage
	lim := 50
	_ = p.call(r, nil, &page, func(w http.ResponseWriter, r *http.Request) {
		p.API.GetClientRequests(w, r, gen.GetClientRequestsParams{Limit: &lim})
	})
	f.Requests = page.Data
	p.render(w, r, "complaint-new", &view{Title: "Raise a complaint", Nav: "account", Error: errMsg, Data: f})
}

func (p *Portal) complaintNew(w http.ResponseWriter, r *http.Request) {
	p.complaintForm(w, r, &complaintForm{RequestID: r.URL.Query().Get("request"), Severity: "quality"}, "")
}

func (p *Portal) complaintCreate(w http.ResponseWriter, r *http.Request) {
	f := &complaintForm{RequestID: r.PostFormValue("request_id"), Severity: r.PostFormValue("severity"),
		Summary: strings.TrimSpace(r.PostFormValue("summary")), Details: r.PostFormValue("details")}
	if len(f.Summary) < 5 {
		p.complaintForm(w, r, f, "Tell us in a sentence what went wrong.")
		return
	}
	in := gen.ComplaintCreate{Severity: gen.ComplaintSeverity(f.Severity), Summary: f.Summary, Details: optString(f.Details)}
	if id, ok := formUUID(r, "request_id"); ok {
		in.RequestId = &id
	}
	var evidence []openapi_types.UUID
	for _, s := range strings.Split(r.PostFormValue("evidence"), ",") {
		if id, ok := parseUUID(s); ok {
			evidence = append(evidence, id)
		}
	}
	if len(evidence) > 0 {
		in.EvidenceDocumentIds = &evidence
	}
	var c gen.Complaint
	if err := p.call(r, in, &c, p.API.PostClientComplaints); err != nil {
		p.complaintForm(w, r, f, errText(err))
		return
	}
	msg := "Complaint " + c.Reference + " received. Our quality team will contact you."
	if in.Severity == "safeguarding" || in.Severity == "loss" {
		msg = "Complaint " + c.Reference + " received. The provider has been stood down while we investigate, and our team will call you."
	}
	p.flash(w, msg)
	p.redirect(w, r, p.Base+"/complaints/"+c.Id.String())
}

func (p *Portal) complaintPage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(r, "id")
	if !ok {
		p.failPage(w, r, &apiError{Status: http.StatusNotFound})
		return
	}
	var c gen.Complaint
	if err := p.call(r, nil, &c, func(w http.ResponseWriter, r *http.Request) { p.API.GetClientComplaintsById(w, r, id) }); err != nil {
		p.failPage(w, r, err)
		return
	}
	p.render(w, r, "complaint", &view{Title: "Complaint " + c.Reference, Nav: "account", Data: c})
}

// ---- account: profile and identity verification

type accountData struct {
	Me  gen.Me
	Sub *gen.Subscription
}

func (p *Portal) accountPage(w http.ResponseWriter, r *http.Request) {
	p.showAccount(w, r, "")
}

func (p *Portal) showAccount(w http.ResponseWriter, r *http.Request, errMsg string) {
	d := &accountData{}
	if err := p.call(r, nil, &d.Me, p.API.GetMe); err != nil {
		p.failPage(w, r, err)
		return
	}
	var sub gen.Subscription
	if p.call(r, nil, &sub, p.API.GetClientSubscription) == nil {
		d.Sub = &sub
	}
	p.render(w, r, "account", &view{Title: "Account", Nav: "account", Me: &d.Me, Error: errMsg, Data: d})
}

func (p *Portal) profileSave(w http.ResponseWriter, r *http.Request) {
	in := gen.MeUpdate{FullName: optString(r.PostFormValue("full_name"))}
	if e := optString(r.PostFormValue("email")); e != nil {
		em := openapi_types.Email(*e)
		in.Email = &em
	}
	if l := r.PostFormValue("language"); l == "en" || l == "sw" {
		lang := gen.Language(l)
		in.PreferredLanguage = &lang
	}
	if err := p.call(r, in, nil, p.API.PatchMe); err != nil {
		p.showAccount(w, r, errText(err))
		return
	}
	p.flash(w, "Saved.")
	p.redirect(w, r, p.Base+"/profile")
}

func (p *Portal) idVerification(w http.ResponseWriter, r *http.Request) {
	doc, ok := formUUID(r, "document_id")
	if !ok {
		p.showAccount(w, r, "Add a photo of your ID first.")
		return
	}
	in := gen.IdVerificationInput{DocumentId: doc, NationalIdNumber: strings.TrimSpace(r.PostFormValue("id_number"))}
	if err := p.call(r, in, nil, p.API.PostClientIdVerification); err != nil {
		p.showAccount(w, r, errText(err))
		return
	}
	p.flash(w, "Thanks. We will check your ID and let you know.")
	p.redirect(w, r, p.Base+"/profile")
}

// uploadTicket and uploadComplete back the small uploader script: the file goes straight to the
// document vault with a presigned URL and never passes through this server.
func (p *Portal) uploadTicket(w http.ResponseWriter, r *http.Request) {
	size, _ := strconv.Atoi(r.PostFormValue("size_bytes"))
	docType := r.PostFormValue("document_type")
	if docType != "national_id" && docType != "passport" {
		docType = "other"
	}
	in := gen.UploadRequest{DocumentType: gen.DocumentType(docType), MimeType: gen.UploadRequestMimeType(r.PostFormValue("mime_type")),
		SizeBytes: size, OriginalFilename: optString(r.PostFormValue("filename"))}
	var ticket gen.UploadTicket
	if err := p.call(r, in, &ticket, p.API.PostDocumentsUploads); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": errText(err)})
		return
	}
	writeJSON(w, http.StatusCreated, ticket)
}

func (p *Portal) uploadComplete(w http.ResponseWriter, r *http.Request) {
	id, _ := pathUUID(r, "id")
	var doc gen.Document
	if err := p.call(r, nil, &doc, func(w http.ResponseWriter, r *http.Request) { p.API.PostDocumentsComplete(w, r, id) }); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": errText(err)})
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ---- addresses

type addressesData struct {
	Addresses []gen.Address
	Areas     []gen.ServiceArea
	Next      string
}

func (p *Portal) addressesPage(w http.ResponseWriter, r *http.Request) {
	p.showAddresses(w, r, "")
}

func (p *Portal) showAddresses(w http.ResponseWriter, r *http.Request, errMsg string) {
	d := &addressesData{Next: r.URL.Query().Get("next")}
	if err := p.call(r, nil, &d.Addresses, p.API.GetClientAddresses); err != nil {
		p.failPage(w, r, err)
		return
	}
	_ = p.call(r, nil, &d.Areas, p.API.GetPublicServiceAreas)
	p.render(w, r, "addresses", &view{Title: "Addresses", Nav: "account", Error: errMsg, Data: d})
}

func (p *Portal) addressCreate(w http.ResponseWriter, r *http.Request) {
	in := gen.AddressInput{Label: strings.TrimSpace(r.PostFormValue("label")), Estate: optString(r.PostFormValue("estate")),
		Street: optString(r.PostFormValue("street")), HouseNumber: optString(r.PostFormValue("house_number")),
		Landmark: optString(r.PostFormValue("landmark")), AccessDetails: optString(r.PostFormValue("access_details"))}
	if in.Label == "" {
		in.Label = "Home"
	}
	area, ok := formUUID(r, "service_area_id")
	if !ok {
		p.showAddresses(w, r, "Choose the area you live in.")
		return
	}
	in.ServiceAreaId = &area
	lat, e1 := strconv.ParseFloat(r.PostFormValue("lat"), 64)
	lng, e2 := strconv.ParseFloat(r.PostFormValue("lng"), 64)
	if e1 == nil && e2 == nil {
		in.Lat, in.Lng = &lat, &lng
	}
	if r.PostFormValue("is_default") == "yes" {
		t := true
		in.IsDefault = &t
	}
	if err := p.call(r, in, nil, p.API.PostClientAddresses); err != nil {
		p.showAddresses(w, r, errText(err))
		return
	}
	p.flash(w, "Address saved.")
	next := p.Base + "/addresses"
	if n := r.PostFormValue("next"); strings.HasPrefix(n, p.Base+"/") && !strings.HasPrefix(n, "//") {
		next = n
	}
	p.redirect(w, r, next)
}

func (p *Portal) addressDefault(w http.ResponseWriter, r *http.Request) {
	id, _ := pathUUID(r, "id")
	t := true
	if err := p.call(r, gen.AddressInput{IsDefault: &t}, nil, func(w http.ResponseWriter, r *http.Request) { p.API.PatchClientAddressesById(w, r, id) }); err != nil {
		p.showAddresses(w, r, errText(err))
		return
	}
	p.redirect(w, r, p.Base+"/addresses")
}

func (p *Portal) addressArchive(w http.ResponseWriter, r *http.Request) {
	id, _ := pathUUID(r, "id")
	if err := p.call(r, nil, nil, func(w http.ResponseWriter, r *http.Request) { p.API.DeleteClientAddressesById(w, r, id) }); err != nil {
		p.showAddresses(w, r, errText(err))
		return
	}
	p.flash(w, "Address removed. Past bookings keep it.")
	p.redirect(w, r, p.Base+"/addresses")
}

// ---- notifications and messages with management

func (p *Portal) notificationsPage(w http.ResponseWriter, r *http.Request) {
	var page gen.NotificationPage
	lim := 50
	if err := p.call(r, nil, &page, func(w http.ResponseWriter, r *http.Request) {
		p.API.GetNotifications(w, r, gen.GetNotificationsParams{Limit: &lim})
	}); err != nil {
		p.failPage(w, r, err)
		return
	}
	p.render(w, r, "notifications", &view{Title: "Notifications", Nav: "inbox", Data: page})
}

func (p *Portal) notificationsReadAll(w http.ResponseWriter, r *http.Request) {
	_ = p.call(r, nil, nil, p.API.PostNotificationsReadAll)
	p.redirect(w, r, p.Base+"/notifications")
}

func (p *Portal) messagesPage(w http.ResponseWriter, r *http.Request) {
	p.showMessages(w, r, "")
}

func (p *Portal) showMessages(w http.ResponseWriter, r *http.Request, errMsg string) {
	var page gen.ConversationPage
	if err := p.call(r, nil, &page, func(w http.ResponseWriter, r *http.Request) {
		p.API.GetConversations(w, r, gen.GetConversationsParams{})
	}); err != nil {
		p.failPage(w, r, err)
		return
	}
	p.render(w, r, "messages", &view{Title: "Messages", Nav: "inbox", Error: errMsg, Data: dict("Page", page, "Request", r.URL.Query().Get("request"))})
}

func (p *Portal) conversationCreate(w http.ResponseWriter, r *http.Request) {
	in := gen.ConversationCreate{Message: strings.TrimSpace(r.PostFormValue("message")), Subject: optString(r.PostFormValue("subject"))}
	if in.Message == "" {
		p.showMessages(w, r, "Write your message first.")
		return
	}
	if id, ok := formUUID(r, "request_id"); ok {
		in.RequestId = &id
	}
	var c gen.Conversation
	if err := p.call(r, in, &c, p.API.PostConversations); err != nil {
		p.showMessages(w, r, errText(err))
		return
	}
	p.redirect(w, r, p.Base+"/messages/"+c.Id.String())
}

func (p *Portal) conversationPage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(r, "id")
	if !ok {
		p.failPage(w, r, &apiError{Status: http.StatusNotFound})
		return
	}
	var page gen.MessagePage
	lim := 100
	if err := p.call(r, nil, &page, func(w http.ResponseWriter, r *http.Request) {
		p.API.GetConversationsMessages(w, r, id, gen.GetConversationsMessagesParams{Limit: &lim})
	}); err != nil {
		p.failPage(w, r, err)
		return
	}
	// The API lists newest first; a chat reads oldest first.
	msgs := page.Data
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	v := &view{Title: "Conversation", Nav: "inbox", Data: dict("ID", id.String(), "Messages", msgs)}
	if isHTMX(r) && r.URL.Query().Get("poll") == "1" {
		p.renderBlock(w, r, "conversation", "thread", v)
		return
	}
	p.render(w, r, "conversation", v)
}

func (p *Portal) messageSend(w http.ResponseWriter, r *http.Request) {
	id, _ := pathUUID(r, "id")
	body := strings.TrimSpace(r.PostFormValue("body"))
	if body != "" {
		if err := p.call(r, gen.MessageCreate{Body: body}, nil, func(w http.ResponseWriter, r *http.Request) { p.API.PostConversationsMessages(w, r, id) }); err != nil {
			p.flash(w, errText(err))
		}
	}
	p.redirect(w, r, p.Base+"/messages/"+id.String())
}
