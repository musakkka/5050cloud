// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"testing"

	"github.com/musakkka/5050cloud/pkg/api"
)

// `5050 up --new` creates an upload service with Deploy Now. It has no
// source until the upload arrives, so the create commit must not deploy it
// (that deployment could only fail with "uploaded source is missing").
func TestNewUploadServiceDoesNotDeployBeforeTheUpload(t *testing.T) {
	e := newEnv(t)
	owner, eid, _ := secOwner(t, e)
	var cs api.CreateServiceResponse
	owner.do("POST", "/api/v1/environments/"+eid+"/services", api.CreateServiceRequest{Name: "web", DeployNow: true,
		Source: api.SourceConfig{Type: "upload"}}, &cs, 200)
	p, ok := e.orch.LastPlan()
	if !ok || p.Deploy {
		t.Fatalf("the create commit of an upload service deploys: %+v", p)
	}
	if len(cs.Services) != 1 || cs.Services[0].Config.Source.Type != "upload" {
		t.Fatalf("service not created: %+v", cs.Services)
	}
}
