package authentication

import (
	"encoding/json"
	"net/http"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/superplanehq/superplane/pkg/models"
)

type ssoProviderInfo struct {
	OrgID        string `json:"orgId"`
	OrgName      string `json:"orgName"`
	ProviderSlug string `json:"providerSlug"`
	DisplayName  string `json:"displayName"`
	LoginURL     string `json:"loginUrl"`
}

// handleSSOProviderLookup implements home-realm discovery for the login screen:
// GET /auth/sso/providers?email=<addr>. It returns only the enabled providers
// whose allowed_email_domains contains the queried email's domain, so it cannot
// be used to enumerate providers that have no domain restriction (those are
// reachable only via a direct org login URL). It is intentionally unauthenticated.
func (a *Handler) handleSSOProviderLookup(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.URL.Query().Get("email"))
	out := []ssoProviderInfo{}

	if email != "" {
		providers, err := models.FindEnabledOIDCProvidersByEmailDomain(emailDomain(email))
		if err != nil {
			log.Errorf("SSO provider lookup failed: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		orgNames := map[string]string{}
		for i := range providers {
			p := &providers[i]
			orgID := p.OrganizationID.String()

			name, ok := orgNames[orgID]
			if !ok {
				if org, err := models.FindOrganizationByID(orgID); err == nil {
					name = org.Name
				}
				orgNames[orgID] = name
			}

			out = append(out, ssoProviderInfo{
				OrgID:        orgID,
				OrgName:      name,
				ProviderSlug: p.Slug,
				DisplayName:  p.DisplayName,
				LoginURL:     "/auth/sso/" + orgID + "/" + p.Slug,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"providers": out}); err != nil {
		log.Errorf("Error encoding SSO provider lookup response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}
