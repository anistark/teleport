/*
Copyright 2021 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/gravitational/trace"
	"github.com/julienschmidt/httprouter"
	kyaml "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/gravitational/teleport/api/client/proto"
	"github.com/gravitational/teleport/api/types"
	"github.com/gravitational/teleport/lib/client"
	"github.com/gravitational/teleport/lib/defaults"
	"github.com/gravitational/teleport/lib/httplib"
	"github.com/gravitational/teleport/lib/reversetunnel"
	"github.com/gravitational/teleport/lib/services"
	"github.com/gravitational/teleport/lib/web/ui"
)

// checkAccessToRegisteredResource checks if calling user has access to at least one registered resource.
func (h *Handler) checkAccessToRegisteredResource(w http.ResponseWriter, r *http.Request, p httprouter.Params, c *SessionContext, site reversetunnel.RemoteSite) (interface{}, error) {
	// Get a client to the Auth Server with the logged in user's identity. The
	// identity of the logged in user is used to fetch the list of resources.
	clt, err := c.GetUserClient(r.Context(), site)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	resourceKinds := []string{types.KindNode, types.KindDatabaseServer, types.KindAppServer, types.KindKubeService, types.KindWindowsDesktop}
	for _, kind := range resourceKinds {
		res, err := clt.ListResources(r.Context(), proto.ListResourcesRequest{
			ResourceType: kind,
			Limit:        1,
		})

		if err != nil {
			// Access denied error is returned when user does not have permissions
			// to read/list a resource kind which can be ignored as this function is not
			// about checking if user has the right perms.
			if trace.IsAccessDenied(err) {
				continue
			}
			return nil, trace.Wrap(err)
		}

		if len(res.Resources) > 0 {
			return checkAccessToRegisteredResourceResponse{
				HasResource: true,
			}, nil
		}
	}

	return checkAccessToRegisteredResourceResponse{
		HasResource: false,
	}, nil
}

func (h *Handler) getRolesHandle(w http.ResponseWriter, r *http.Request, params httprouter.Params, ctx *SessionContext) (interface{}, error) {
	clt, err := ctx.GetClient()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return getRoles(clt)
}

func getRoles(clt resourcesAPIGetter) ([]ui.ResourceItem, error) {
	roles, err := clt.GetRoles(context.TODO())
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return ui.NewRoles(roles)
}

func (h *Handler) deleteRole(w http.ResponseWriter, r *http.Request, params httprouter.Params, ctx *SessionContext) (interface{}, error) {
	clt, err := ctx.GetClient()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	roleName := params.ByName("name")
	if err := clt.DeleteRole(r.Context(), roleName); err != nil {
		return nil, trace.Wrap(err)
	}

	return OK(), nil
}

func (h *Handler) upsertRoleHandle(w http.ResponseWriter, r *http.Request, params httprouter.Params, ctx *SessionContext) (interface{}, error) {
	clt, err := ctx.GetClient()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	var req ui.ResourceItem
	if err := httplib.ReadJSON(r, &req); err != nil {
		return nil, trace.Wrap(err)
	}

	return upsertRole(r.Context(), clt, req.Content, r.Method)
}

func upsertRole(ctx context.Context, clt resourcesAPIGetter, content, httpMethod string) (*ui.ResourceItem, error) {
	extractedRes, err := ExtractResourceAndValidate(content)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	if extractedRes.Kind != types.KindRole {
		return nil, trace.BadParameter("resource kind %q is invalid", extractedRes.Kind)
	}

	_, err = clt.GetRole(ctx, extractedRes.Metadata.Name)
	if err := CheckResourceUpsertableByError(err, httpMethod, extractedRes.Metadata.Name); err != nil {
		return nil, trace.Wrap(err)
	}

	role, err := services.UnmarshalRole(extractedRes.Raw)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	if err := clt.UpsertRole(ctx, role); err != nil {
		return nil, trace.Wrap(err)
	}

	return ui.NewResourceItem(role)
}

func (h *Handler) getGithubConnectorsHandle(w http.ResponseWriter, r *http.Request, params httprouter.Params, ctx *SessionContext) (interface{}, error) {
	clt, err := ctx.GetClient()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return getGithubConnectors(r.Context(), clt)
}

func getGithubConnectors(ctx context.Context, clt resourcesAPIGetter) ([]ui.ResourceItem, error) {
	connectors, err := clt.GetGithubConnectors(ctx, true)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return ui.NewGithubConnectors(connectors)
}

func (h *Handler) deleteGithubConnector(w http.ResponseWriter, r *http.Request, params httprouter.Params, ctx *SessionContext) (interface{}, error) {
	clt, err := ctx.GetClient()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	connectorName := params.ByName("name")
	if err := clt.DeleteGithubConnector(r.Context(), connectorName); err != nil {
		return nil, trace.Wrap(err)
	}

	return OK(), nil
}

func (h *Handler) upsertGithubConnectorHandle(w http.ResponseWriter, r *http.Request, params httprouter.Params, ctx *SessionContext) (interface{}, error) {
	clt, err := ctx.GetClient()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	var req ui.ResourceItem
	if err := httplib.ReadJSON(r, &req); err != nil {
		return nil, trace.Wrap(err)
	}

	return upsertGithubConnector(r.Context(), clt, req.Content, r.Method)
}

func upsertGithubConnector(ctx context.Context, clt resourcesAPIGetter, content, httpMethod string) (*ui.ResourceItem, error) {
	extractedRes, err := ExtractResourceAndValidate(content)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	if extractedRes.Kind != types.KindGithubConnector {
		return nil, trace.BadParameter("resource kind %q is invalid", extractedRes.Kind)
	}

	_, err = clt.GetGithubConnector(ctx, extractedRes.Metadata.Name, false)
	if err := CheckResourceUpsertableByError(err, httpMethod, extractedRes.Metadata.Name); err != nil {
		return nil, trace.Wrap(err)
	}

	connector, err := services.UnmarshalGithubConnector(extractedRes.Raw)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	if err := clt.UpsertGithubConnector(ctx, connector); err != nil {
		return nil, trace.Wrap(err)
	}

	return ui.NewResourceItem(connector)
}

func (h *Handler) getTrustedClustersHandle(w http.ResponseWriter, r *http.Request, params httprouter.Params, ctx *SessionContext) (interface{}, error) {
	clt, err := ctx.GetClient()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return getTrustedClusters(r.Context(), clt)
}

func getTrustedClusters(ctx context.Context, clt resourcesAPIGetter) ([]ui.ResourceItem, error) {
	trustedClusters, err := clt.GetTrustedClusters(ctx)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return ui.NewTrustedClusters(trustedClusters)
}

func (h *Handler) deleteTrustedCluster(w http.ResponseWriter, r *http.Request, params httprouter.Params, ctx *SessionContext) (interface{}, error) {
	clt, err := ctx.GetClient()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	tcName := params.ByName("name")
	if err := clt.DeleteTrustedCluster(r.Context(), tcName); err != nil {
		return nil, trace.Wrap(err)
	}

	return OK(), nil
}

func (h *Handler) upsertTrustedClusterHandle(w http.ResponseWriter, r *http.Request, params httprouter.Params, ctx *SessionContext) (interface{}, error) {
	clt, err := ctx.GetClient()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	var req ui.ResourceItem
	if err := httplib.ReadJSON(r, &req); err != nil {
		return nil, trace.Wrap(err)
	}

	return upsertTrustedCluster(r.Context(), clt, req.Content, r.Method)
}

func upsertTrustedCluster(ctx context.Context, clt resourcesAPIGetter, content, httpMethod string) (*ui.ResourceItem, error) {
	extractedRes, err := ExtractResourceAndValidate(content)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	if extractedRes.Kind != types.KindTrustedCluster {
		return nil, trace.BadParameter("resource kind %q is invalid", extractedRes.Kind)
	}

	_, err = clt.GetTrustedCluster(ctx, extractedRes.Metadata.Name)
	if err := CheckResourceUpsertableByError(err, httpMethod, extractedRes.Metadata.Name); err != nil {
		return nil, trace.Wrap(err)
	}

	tc, err := services.UnmarshalTrustedCluster(extractedRes.Raw)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	_, err = clt.UpsertTrustedCluster(ctx, tc)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	return ui.NewResourceItem(tc)
}

// CheckResourceUpsertableByError checks if the resource is upsertable by the state of error with
// the request http method used.
func CheckResourceUpsertableByError(err error, httpMethod, resourceName string) error {
	if err != nil && !trace.IsNotFound(err) {
		return trace.Wrap(err)
	}

	exists := err == nil
	if exists && httpMethod == http.MethodPost {
		return trace.AlreadyExists("resource name %q already exists", resourceName)
	}

	if !exists && httpMethod == http.MethodPut {
		return trace.NotFound("cannot find resource with name %q", resourceName)
	}

	return nil
}

// ExtractResourceAndValidate extracts resource information from given string and validates basic fields.
func ExtractResourceAndValidate(yaml string) (*services.UnknownResource, error) {
	var unknownRes services.UnknownResource
	reader := strings.NewReader(yaml)
	decoder := kyaml.NewYAMLOrJSONDecoder(reader, 32*1024)

	if err := decoder.Decode(&unknownRes); err != nil {
		return nil, trace.BadParameter("not a valid resource declaration")
	}

	if err := unknownRes.Metadata.CheckAndSetDefaults(); err != nil {
		return nil, trace.Wrap(err)
	}

	return &unknownRes, nil
}

// listResources gets a list of resources depending on the type of resource.
func listResources(clt resourcesAPIGetter, r *http.Request, resourceKind string) (*types.ListResourcesResponse, error) {
	values := r.URL.Query()

	limit, err := queryLimitAsInt32(values, "limit", defaults.MaxIterationLimit)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	sortBy := types.GetSortByFromString(values.Get("sort"))

	startKey := values.Get("startKey")
	req := proto.ListResourcesRequest{
		ResourceType:        resourceKind,
		Limit:               limit,
		StartKey:            startKey,
		SortBy:              sortBy,
		PredicateExpression: values.Get("query"),
		SearchKeywords:      client.ParseSearchKeywords(values.Get("search"), ' '),
		UseSearchAsRoles:    values.Get("searchAsRoles") == "yes",
	}

	return clt.ListResources(r.Context(), req)
}

type listResourcesGetResponse struct {
	// Items is a list of resources retrieved.
	Items interface{} `json:"items"`
	// StartKey is the position to resume search events.
	StartKey string `json:"startKey"`
	// TotalCount is the total count of resources available
	// after filter.
	TotalCount int `json:"totalCount"`
}

type checkAccessToRegisteredResourceResponse struct {
	// HasResource is a flag to indicate if user has any access
	// to a registered resource or not.
	HasResource bool `json:"hasResource"`
}

type resourcesAPIGetter interface {
	// GetRole returns role by name
	GetRole(ctx context.Context, name string) (types.Role, error)
	// GetRoles returns a list of roles
	GetRoles(ctx context.Context) ([]types.Role, error)
	// UpsertRole creates or updates role
	UpsertRole(ctx context.Context, role types.Role) error
	// UpsertGithubConnector creates or updates a Github connector
	UpsertGithubConnector(ctx context.Context, connector types.GithubConnector) error
	// GetGithubConnectors returns all configured Github connectors
	GetGithubConnectors(ctx context.Context, withSecrets bool) ([]types.GithubConnector, error)
	// GetGithubConnector returns the specified Github connector
	GetGithubConnector(ctx context.Context, id string, withSecrets bool) (types.GithubConnector, error)
	// DeleteGithubConnector deletes the specified Github connector
	DeleteGithubConnector(ctx context.Context, id string) error
	// UpsertTrustedCluster creates or updates a TrustedCluster in the backend.
	UpsertTrustedCluster(ctx context.Context, tc types.TrustedCluster) (types.TrustedCluster, error)
	// GetTrustedCluster returns a single TrustedCluster by name.
	GetTrustedCluster(ctx context.Context, name string) (types.TrustedCluster, error)
	// GetTrustedClusters returns all TrustedClusters in the backend.
	GetTrustedClusters(ctx context.Context) ([]types.TrustedCluster, error)
	// DeleteTrustedCluster removes a TrustedCluster from the backend by name.
	DeleteTrustedCluster(ctx context.Context, name string) error
	// ListResoures returns a paginated list of resources.
	ListResources(ctx context.Context, req proto.ListResourcesRequest) (*types.ListResourcesResponse, error)
}
