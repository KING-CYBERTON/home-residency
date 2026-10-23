package provider

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api"
	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/sms"
)

// Onboarding (report §5.1): apply for a category, upload documents, add referees, submit, follow
// vetting and training, sign the contract, keep the profile and payout number current.

func pathID(r *http.Request, key string) (gen.Id, bool) {
	id, err := uuid.Parse(chi.URLParam(r, key))
	return gen.Id(id), err == nil
}

func (p *Portal) categoryNames(r *http.Request) map[openapi_types.UUID]string {
	out := map[openapi_types.UUID]string{}
	cats, _ := call[[]gen.Category](r, nil, p.api.GetPublicCategories)
	for _, c := range cats {
		out[c.Id] = c.Name
	}
	return out
}

// ---- applications

type applicationsPage struct {
	Apps  []gen.ApplicationSummary
	Names map[openapi_types.UUID]string
}

func (p *Portal) applications(w http.ResponseWriter, r *http.Request) {
	apps, err := call[[]gen.ApplicationSummary](r, nil, p.api.GetProviderApplications)
	if err != nil {
		p.problem(w, r, err)
		return
	}
	p.render(w, r, http.StatusOK, page{name: "applications", title: "My applications", nav: "more"}, applicationsPage{Apps: apps, Names: p.categoryNames(r)})
}

type newApplicationPage struct {
	Categories []gen.Category
	Selected   string
}

func (p *Portal) newApplication(w http.ResponseWriter, r *http.Request) {
	cats, err := call[[]gen.Category](r, nil, p.api.GetPublicCategories)
	if err != nil {
		p.problem(w, r, err)
		return
	}
	p.render(w, r, http.StatusOK, page{name: "application_new", title: "Apply for work", nav: "more"}, newApplicationPage{Categories: cats, Selected: r.URL.Query().Get("category")})
}

func (p *Portal) createApplication(w http.ResponseWriter, r *http.Request) {
	cat, err := uuid.Parse(r.PostFormValue("category_id"))
	if err != nil {
		p.done(w, r, portal+"/applications/new", "! Choose the kind of work you want to apply for.")
		return
	}
	app, err := call[gen.Application](r, gen.ApplicationCreate{CategoryId: cat}, p.api.PostProviderApplications)
	if err != nil {
		p.failed(w, r, portal+"/applications/new", err)
		return
	}
	p.done(w, r, portal+"/applications/"+app.Id.String(), "Application started. Upload each document below, then submit.")
}

type applicationPage struct {
	App      gen.Application
	Category string
	Editable bool
	Docs     []gen.Document // uploaded documents that could fill an item
}

func editable(s gen.ApplicationStatus) bool {
	return s == gen.ApplicationStatusDraft || s == gen.ApplicationStatusIncomplete || s == gen.ApplicationStatusDeferred
}

func (p *Portal) application(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	app, err := call[gen.Application](r, nil, func(w http.ResponseWriter, rq *http.Request) { p.api.GetProviderApplicationsById(w, rq, id) })
	if err != nil {
		p.problem(w, r, err)
		return
	}
	docs, _ := call[[]gen.Document](r, nil, p.api.GetProviderDocuments)
	usable := docs[:0]
	for _, d := range docs {
		if d.Status == gen.DocumentStatusUploaded || d.Status == gen.DocumentStatusVerified {
			usable = append(usable, d)
		}
	}
	p.render(w, r, http.StatusOK, page{name: "application", title: "Application " + app.Reference, nav: "more"},
		applicationPage{App: app, Category: p.categoryNames(r)[app.CategoryId], Editable: editable(app.Status), Docs: usable})
}

func (p *Portal) addReferee(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	back := portal + "/applications/" + id.String()
	in := gen.RefereeInput{
		FullName:     strings.TrimSpace(r.PostFormValue("full_name")),
		Phone:        normalisePhone(r.PostFormValue("phone")),
		Relationship: strings.TrimSpace(r.PostFormValue("relationship")),
	}
	if v := strings.TrimSpace(r.PostFormValue("employer_name")); v != "" {
		in.EmployerName = &v
	}
	in.EmploymentFrom, in.EmploymentTo = formDate(r, "employment_from"), formDate(r, "employment_to")
	if err := do(r, in, func(w http.ResponseWriter, rq *http.Request) { p.api.PostProviderApplicationsReferees(w, rq, id) }); err != nil {
		p.failed(w, r, back, err)
		return
	}
	p.done(w, r, back, "Referee added.")
}

func (p *Portal) removeReferee(w http.ResponseWriter, r *http.Request) {
	id, ok1 := pathID(r, "id")
	rid, ok2 := pathID(r, "rid")
	if !ok1 || !ok2 {
		http.NotFound(w, r)
		return
	}
	back := portal + "/applications/" + id.String()
	if err := do(r, nil, func(w http.ResponseWriter, rq *http.Request) {
		p.api.DeleteProviderApplicationsRefereesById(w, rq, id, rid)
	}); err != nil {
		p.failed(w, r, back, err)
		return
	}
	p.done(w, r, back, "Referee removed.")
}

func (p *Portal) submitApplication(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	back := portal + "/applications/" + id.String()
	if err := do(r, nil, func(w http.ResponseWriter, rq *http.Request) { p.api.PostProviderApplicationsSubmit(w, rq, id) }); err != nil {
		p.failed(w, r, back, err)
		return
	}
	p.done(w, r, back, "Submitted. Our vetting team will check your documents and call your referees. We will SMS you with each step.")
}

func (p *Portal) withdrawApplication(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	back := portal + "/applications/" + id.String()
	if err := do(r, nil, func(w http.ResponseWriter, rq *http.Request) { p.api.PostProviderApplicationsWithdraw(w, rq, id) }); err != nil {
		p.failed(w, r, back, err)
		return
	}
	p.done(w, r, back, "Application withdrawn.")
}

// ---- documents

type documentsPage struct {
	Docs  []gen.Document
	Types []string
}

// providerDocTypes are the vault types a provider may upload (signed contracts are made by signing).
var providerDocTypes = []string{"national_id", "passport", "passport_photo", "good_conduct_certificate", "reference_letter",
	"academic_certificate", "trade_certificate", "trade_licence", "first_aid_certificate", "medical_certificate", "kra_pin",
	"nssf_card", "shif_card", "other"}

func (p *Portal) documents(w http.ResponseWriter, r *http.Request) {
	docs, err := call[[]gen.Document](r, nil, p.api.GetProviderDocuments)
	if err != nil {
		p.problem(w, r, err)
		return
	}
	p.render(w, r, http.StatusOK, page{name: "documents", title: "My documents", nav: "more"}, documentsPage{Docs: docs, Types: providerDocTypes})
}

// uploadTicket is called by portal.js before it uploads a file straight to the vault: the file never
// passes through this server, and the vault checks size and type when the upload is confirmed.
func (p *Portal) uploadTicket(w http.ResponseWriter, r *http.Request) {
	var in gen.UploadRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&in); err != nil {
		http.Error(w, `{"code":"bad_request","message":"Send the file details as JSON"}`, http.StatusBadRequest)
		return
	}
	code, _, raw := api.Invoke(r, in, p.api.PostDocumentsUploads)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(raw)
}

// completeUpload confirms an upload made by portal.js and, from an application page, attaches it
// to the checklist item it was uploaded for.
func (p *Portal) completeUpload(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	back := portal + "/documents"
	if app, err := uuid.Parse(r.PostFormValue("application_id")); err == nil {
		back = portal + "/applications/" + app.String()
	}
	if err := do(r, nil, func(w http.ResponseWriter, rq *http.Request) { p.api.PostDocumentsComplete(w, rq, id) }); err != nil {
		p.failed(w, r, back, err)
		return
	}
	if r.PostFormValue("item_id") == "" {
		p.done(w, r, back, "Document uploaded. We will check it and let you know.")
		return
	}
	p.attach(w, r, id)
}

// attachDocument fills a checklist item with a document already in the vault (no upload needed).
func (p *Portal) attachDocument(w http.ResponseWriter, r *http.Request) {
	doc, err := uuid.Parse(r.PostFormValue("document_id"))
	if err != nil {
		p.done(w, r, portal+"/applications/"+chi.URLParam(r, "id"), "! Choose one of your documents.")
		return
	}
	p.attach(w, r, doc)
}

func (p *Portal) attach(w http.ResponseWriter, r *http.Request, doc uuid.UUID) {
	appID, err1 := uuid.Parse(r.PostFormValue("application_id"))
	if v := chi.URLParam(r, "id"); v != "" && r.PostFormValue("application_id") == "" {
		appID, err1 = uuid.Parse(v)
	}
	itemID, err2 := uuid.Parse(r.PostFormValue("item_id"))
	if err1 != nil || err2 != nil {
		p.done(w, r, portal+"/documents", "! That checklist item was not found.")
		return
	}
	back := portal + "/applications/" + appID.String()
	links := []struct {
		ApplicationItemId openapi_types.UUID `json:"application_item_id"`
		DocumentId        openapi_types.UUID `json:"document_id"`
	}{{ApplicationItemId: itemID, DocumentId: doc}}
	in := gen.ApplicationUpdate{ItemDocuments: &links}
	if err := do(r, in, func(w http.ResponseWriter, rq *http.Request) { p.api.PatchProviderApplicationsById(w, rq, appID) }); err != nil {
		p.failed(w, r, back, err)
		return
	}
	p.done(w, r, back, "Document added to your application.")
}

// ---- training and contract

func (p *Portal) training(w http.ResponseWriter, r *http.Request) {
	list, err := call[[]gen.TrainingEnrolment](r, nil, p.api.GetProviderTrainings)
	if err != nil {
		p.problem(w, r, err)
		return
	}
	p.render(w, r, http.StatusOK, page{name: "training", title: "Training", nav: "more"}, list)
}

type contractPage struct {
	Contract gen.Contract
	Status   gen.ProviderStatus
	OtpID    string
	Error    string
	// ByCode offers signing with an SMS code (SMS is on and the provider has a phone on file);
	// ByPassword with the account password.
	ByCode, ByPassword bool
}

func (p *Portal) contractData(r *http.Request) (contractPage, error) {
	c, err := call[gen.Contract](r, nil, p.api.GetProviderContract)
	if err != nil {
		return contractPage{}, err
	}
	me, err := call[gen.Me](r, nil, p.api.GetMe)
	if err != nil {
		return contractPage{}, err
	}
	out := contractPage{Contract: c, ByCode: sms.Enabled(p.api.SMS) && me.Phone != nil, ByPassword: me.HasPassword != nil && *me.HasPassword}
	if me.ProviderStatus != nil {
		out.Status = *me.ProviderStatus
	}
	return out, nil
}

func (p *Portal) contract(w http.ResponseWriter, r *http.Request) {
	d, err := p.contractData(r)
	if err != nil {
		p.problem(w, r, err)
		return
	}
	p.render(w, r, http.StatusOK, page{name: "contract", title: "Contract", nav: "more"}, d)
}

// contractCode sends an SMS code to the provider's own phone; typing it back is the signature.
func (p *Portal) contractCode(w http.ResponseWriter, r *http.Request) {
	d, err := p.contractData(r)
	if err != nil {
		p.problem(w, r, err)
		return
	}
	me, err := call[gen.Me](r, nil, p.api.GetMe)
	if err != nil || me.Phone == nil {
		p.failed(w, r, portal+"/contract", err)
		return
	}
	sent, err := call[otpSent](r, gen.OtpRequest{Phone: *me.Phone, Purpose: gen.VerifyPhone}, p.api.PostAuthOtpRequest)
	if err != nil {
		d.Error = message(err)
	}
	d.OtpID = sent.OtpID
	p.render(w, r, http.StatusOK, page{name: "contract", title: "Contract", nav: "more"}, d)
}

// contractSign signs with the SMS code, or with the account password when the form sends one.
func (p *Portal) contractSign(w http.ResponseWriter, r *http.Request) {
	in := gen.ContractSign{ContractVersion: r.PostFormValue("contract_version"), CodeOfConductVersion: r.PostFormValue("code_of_conduct_version")}
	otpID := ""
	if pw := r.PostFormValue("password"); pw != "" {
		in.Password = &pw
	} else {
		otp, err := uuid.Parse(r.PostFormValue("otp_id"))
		if err != nil {
			p.redirect(w, r, portal+"/contract")
			return
		}
		code := strings.TrimSpace(r.PostFormValue("code"))
		in.OtpId, in.Code, otpID = &otp, &code, otp.String()
	}
	if err := do(r, in, p.api.PostProviderContractSign); err != nil {
		d, derr := p.contractData(r)
		if derr != nil {
			p.problem(w, r, derr)
			return
		}
		d.OtpID, d.Error = otpID, message(err)
		p.render(w, r, http.StatusUnprocessableEntity, page{name: "contract", title: "Contract", nav: "more"}, d)
		return
	}
	p.done(w, r, portal+"/", "Contract signed. Management will activate your account and you will start receiving job offers.")
}

// ---- profile

type profilePage struct {
	Me      gen.Me
	Profile gen.ProviderProfile
	Areas   []gen.ServiceArea
	InArea  map[openapi_types.UUID]bool
}

func (p *Portal) profile(w http.ResponseWriter, r *http.Request) {
	prof, err := call[gen.ProviderProfile](r, nil, p.api.GetProviderProfile)
	if err != nil {
		p.problem(w, r, err)
		return
	}
	areas, _ := call[[]gen.ServiceArea](r, nil, p.api.GetPublicServiceAreas)
	in := map[openapi_types.UUID]bool{}
	if prof.ServiceAreaIds != nil {
		for _, a := range *prof.ServiceAreaIds {
			in[a] = true
		}
	}
	me, err := call[gen.Me](r, nil, p.api.GetMe)
	if err != nil {
		p.problem(w, r, err)
		return
	}
	p.render(w, r, http.StatusOK, page{name: "profile", title: "My profile", nav: "more"}, profilePage{Me: me, Profile: prof, Areas: areas, InArea: in})
}

// savePassword sets the email and password the provider signs in with.
func (p *Portal) savePassword(w http.ResponseWriter, r *http.Request) {
	if err := setSignIn(r, p.api); err != nil {
		p.failed(w, r, portal+"/profile#sign-in", err)
		return
	}
	p.done(w, r, portal+"/profile", "Password saved. You can now sign in with your email and password.")
}

func (p *Portal) saveProfile(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	var in gen.ProviderProfileUpdate
	str := func(k string) *string {
		v := strings.TrimSpace(r.PostFormValue(k))
		if v == "" {
			return nil
		}
		return &v
	}
	in.Bio = str("bio")
	in.KraPin = str("kra_pin")
	in.NationalIdNumber = str("national_id_number")
	if v := str("payout_msisdn"); v != nil {
		n := normalisePhone(*v)
		in.PayoutMsisdn = &n
	}
	if v := str("years_experience"); v != nil {
		if n, err := strconv.Atoi(*v); err == nil {
			in.YearsExperience = &n
		}
	}
	if v := str("languages"); v != nil {
		var langs []string
		for _, l := range strings.Split(*v, ",") {
			if l = strings.TrimSpace(l); l != "" {
				langs = append(langs, l)
			}
		}
		in.Languages = &langs
	}
	in.DateOfBirth = formDate(r, "date_of_birth")
	if v := str("home_service_area_id"); v != nil {
		if id, err := uuid.Parse(*v); err == nil {
			in.HomeServiceAreaId = &id
		}
	}
	if r.PostFormValue("areas_shown") == "1" { // an unticked box sends nothing, so only replace what the form showed
		areas := []openapi_types.UUID{}
		for _, v := range r.PostForm["service_area_ids"] {
			if id, err := uuid.Parse(v); err == nil {
				areas = append(areas, id)
			}
		}
		in.ServiceAreaIds = &areas
	}
	if err := do(r, in, p.api.PatchProviderProfile); err != nil {
		p.failed(w, r, portal+"/profile", err)
		return
	}
	p.done(w, r, portal+"/profile", "Profile saved.")
}

// formDate reads a <input type="date"> value.
func formDate(r *http.Request, key string) *openapi_types.Date {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(r.PostFormValue(key)))
	if err != nil {
		return nil
	}
	return &openapi_types.Date{Time: t}
}

// setSignIn saves a changed email address, then the new password.
func setSignIn(r *http.Request, srv *api.Server) error {
	if e := strings.TrimSpace(r.PostFormValue("email")); e != "" {
		me, err := call[gen.Me](r, nil, srv.GetMe)
		if err != nil {
			return err
		}
		if me.Email == nil || !strings.EqualFold(*me.Email, e) {
			em := openapi_types.Email(e)
			if err := do(r, gen.MeUpdate{Email: &em}, srv.PatchMe); err != nil {
				return err
			}
		}
	}
	in := gen.PasswordChange{NewPassword: r.PostFormValue("new_password")}
	if cur := r.PostFormValue("current_password"); cur != "" {
		in.CurrentPassword = &cur
	}
	return do(r, in, srv.PostMePassword)
}
