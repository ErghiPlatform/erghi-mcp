package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/ErghiPlatform/erghi-mcp/internal/erghi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxSpecBytes keeps a pasted OpenAPI document to a sane size.
const maxSpecBytes = 5 << 20

// ── OpenAPI integrations (the customer's APIs the assistant can call) ────

type integrationIDInput struct {
	IntegrationID string `json:"integrationId"`
}

type listIntegrationsInput struct {
	IncludeEndpoints bool `json:"includeEndpoints,omitempty" jsonschema:"Also return each integration's endpoints, bindings and AI instructions"`
}

type addIntegrationInput struct {
	Name        string `json:"name" jsonschema:"Integration name the team will recognise, e.g. Orders API"`
	BaseURL     string `json:"baseUrl,omitempty" jsonschema:"https base URL the calls go to; defaults to the spec's first server"`
	SpecContent string `json:"specContent" jsonschema:"The whole OpenAPI 3 (or Swagger 2) document, JSON or YAML"`
}

type replaceSpecInput struct {
	IntegrationID string `json:"integrationId"`
	SpecContent   string `json:"specContent" jsonschema:"The new OpenAPI document, JSON or YAML"`
	Confirm       bool   `json:"confirm" jsonschema:"Must be true. Endpoints missing from the new spec are removed with their tool context; only set it after the user agreed."`
}

type baseURLInput struct {
	IntegrationID string `json:"integrationId"`
	BaseURL       string `json:"baseUrl" jsonschema:"New https base URL. Refused for keys once the integration has saved credentials."`
}

type requestBinding struct {
	In       string `json:"in" jsonschema:"header, query, path or body"`
	Name     string `json:"name" jsonschema:"Parameter name; for body a dotted path like transfer.user_id"`
	Value    string `json:"value" jsonschema:"Literal with optional placeholders: {{identity.sub}}, {{secret.<key>}}, {{metadata.<key>}}, {{conversation.id}}. Never put a real secret here; use a {{secret.*}} placeholder."`
	Required *bool  `json:"required,omitempty" jsonschema:"Default true: if a placeholder has no value the call is not made and the visitor is asked to sign in"`
}

type toolContextInput struct {
	IntegrationID   string           `json:"integrationId"`
	EndpointID      string           `json:"endpointId,omitempty" jsonschema:"Endpoint id from get_integration; omit to set the integration-wide context"`
	RequestBindings []requestBinding `json:"requestBindings,omitempty" jsonschema:"Parameters filled from the conversation instead of by the model (max 30). Replaces the current list; omit or send [] to clear."`
	AIInstructions  *string          `json:"aiInstructions,omitempty" jsonschema:"When and how the assistant should use these calls (max 4000 chars). Appended to the tool description."`
}

type setIntegrationActiveInput struct {
	IntegrationID string `json:"integrationId"`
	IsActive      bool   `json:"isActive" jsonschema:"false hides all of this integration's tools from the assistant"`
}

type testEndpointInput struct {
	IntegrationID       string            `json:"integrationId"`
	OperationID         string            `json:"operationId" jsonschema:"operationId from get_integration"`
	PathParameters      map[string]string `json:"pathParameters,omitempty"`
	QueryParameters     map[string]string `json:"queryParameters,omitempty"`
	Headers             map[string]string `json:"headers,omitempty"`
	Body                string            `json:"body,omitempty" jsonschema:"Raw request body (JSON text)"`
	UseSavedCredentials *bool             `json:"useSavedCredentials,omitempty" jsonschema:"Default true"`
	Confirm             bool              `json:"confirm,omitempty" jsonschema:"Required for anything but GET, since it runs against the customer's real system. Only set after the user agreed."`
}

type deleteIntegrationInput struct {
	IntegrationID string `json:"integrationId"`
	Confirm       bool   `json:"confirm" jsonschema:"Must be true. Only set it after the user has explicitly agreed."`
}

// integrationSummary is an integration as list_integrations?includeEndpoints returns it.
type integrationSummary struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	BaseURL         string           `json:"baseUrl"`
	IsActive        bool             `json:"isActive"`
	EndpointCount   int              `json:"endpointCount"`
	RequestBindings []requestBinding `json:"requestBindings"`
	AIInstructions  *string          `json:"aiInstructions"`
	Endpoints       []endpointInfo   `json:"endpoints"`
}

type endpointInfo struct {
	ID              string           `json:"id"`
	OperationID     string           `json:"operationId"`
	Method          string           `json:"method"`
	Path            string           `json:"path"`
	Summary         *string          `json:"summary"`
	ParametersJSON  *string          `json:"parametersJson"`
	HasRequestBody  bool             `json:"hasRequestBody"`
	LLMToolSchema   *string          `json:"llmToolSchema"`
	RequestBindings []requestBinding `json:"requestBindings"`
	AIInstructions  *string          `json:"aiInstructions"`
}

func listIntegrations(ctx context.Context, api *erghi.Client) ([]integrationSummary, error) {
	raw, err := api.Do(ctx, http.MethodGet, "/api/v1/integrations", url.Values{"includeEndpoints": {"true"}}, nil)
	if err != nil {
		return nil, err
	}
	var out []integrationSummary
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("reading integrations: %w", err)
	}
	return out, nil
}

func findIntegration(ctx context.Context, api *erghi.Client, integrationID string) (*integrationSummary, error) {
	all, err := listIntegrations(ctx, api)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if strings.EqualFold(all[i].ID, integrationID) {
			return &all[i], nil
		}
	}
	return nil, fmt.Errorf("no integration with id %s", integrationID)
}

func specFile(content string) (erghi.File, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return erghi.File{}, fmt.Errorf("specContent is required")
	}
	if len(content) > maxSpecBytes {
		return erghi.File{}, fmt.Errorf("specContent is larger than %d bytes", maxSpecBytes)
	}
	name := "openapi.yaml"
	if strings.HasPrefix(content, "{") {
		name = "openapi.json"
	}
	return erghi.File{Field: "specFile", Name: name, Content: []byte(content)}, nil
}

func registerIntegrations(r *registry) {
	const base = "/api/v1/integrations"
	add(r, read, needIntegrations, &mcp.Tool{
		Name:        "list_integrations",
		Description: "List the customer APIs (OpenAPI integrations) the assistant can call during a chat. With includeEndpoints, also each endpoint's bindings and AI instructions.",
	}, func(ctx context.Context, in listIntegrationsInput) (any, error) {
		q := url.Values{}
		if in.IncludeEndpoints {
			q.Set("includeEndpoints", "true")
		}
		return r.api.Do(ctx, http.MethodGet, base, q, nil)
	})
	add(r, read, needIntegrations, &mcp.Tool{
		Name:        "get_integration",
		Description: "One integration with its endpoints (ids, methods, paths, parameters), request bindings and AI instructions. Credentials are never returned.",
	}, func(ctx context.Context, in integrationIDInput) (any, error) {
		iid, err := id("integrationId", in.IntegrationID)
		if err != nil {
			return nil, err
		}
		return findIntegration(ctx, r.api, iid)
	})
	add(r, write, needIntegrations, &mcp.Tool{
		Name: "add_integration_from_openapi",
		Description: "Connect a customer API from its OpenAPI document so the assistant can call its endpoints as tools. " +
			"It starts with no credentials; a person adds those in the admin app. Then set tool context (set_integration_tool_context) and action policies.",
	}, func(ctx context.Context, in addIntegrationInput) (any, error) {
		if strings.TrimSpace(in.Name) == "" {
			return nil, fmt.Errorf("name is required")
		}
		f, err := specFile(in.SpecContent)
		if err != nil {
			return nil, err
		}
		fields := map[string]string{"integrationName": in.Name}
		if in.BaseURL != "" {
			fields["baseUrl"] = in.BaseURL
		}
		return r.api.DoMultipart(ctx, http.MethodPost, base+"/upload-spec", fields, f)
	})
	add(r, destructive, needIntegrations, &mcp.Tool{
		Name:        "replace_integration_spec",
		Description: "Replace an integration's OpenAPI document. Endpoints no longer in it are removed along with their tool context. Needs confirm=true.",
	}, func(ctx context.Context, in replaceSpecInput) (any, error) {
		if err := requireConfirm(in.Confirm); err != nil {
			return nil, err
		}
		iid, err := id("integrationId", in.IntegrationID)
		if err != nil {
			return nil, err
		}
		f, err := specFile(in.SpecContent)
		if err != nil {
			return nil, err
		}
		return r.api.DoMultipart(ctx, http.MethodPost, base+"/"+iid+"/replace-spec", nil, f)
	})
	add(r, write, needIntegrations, &mcp.Tool{
		Name:        "refresh_integration_endpoints",
		Description: "Re-read the endpoints from the integration's stored OpenAPI document.",
	}, func(ctx context.Context, in integrationIDInput) (any, error) {
		iid, err := id("integrationId", in.IntegrationID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPost, base+"/"+iid+"/refresh-endpoints", nil, nil)
	})
	add(r, write, needIntegrations, &mcp.Tool{
		Name:        "update_integration_base_url",
		Description: "Change where an integration's calls go. A key can't do this once the integration has saved credentials (a person does it in the admin app), so credentials can't be redirected.",
	}, func(ctx context.Context, in baseURLInput) (any, error) {
		iid, err := id("integrationId", in.IntegrationID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPatch, base+"/"+iid, nil, map[string]string{"baseUrl": in.BaseURL})
	})
	add(r, write, needIntegrations, &mcp.Tool{
		Name: "set_integration_tool_context",
		Description: "Set request bindings (parameters filled from the signed-in user, host-app secrets or metadata instead of by the model) and AI instructions, " +
			"for the whole integration or one endpoint. Bind every parameter that identifies the user to {{identity.sub}} so the model can't choose it. " +
			"Read get_erghi_guide topic=integrations first.",
	}, func(ctx context.Context, in toolContextInput) (any, error) {
		iid, err := id("integrationId", in.IntegrationID)
		if err != nil {
			return nil, err
		}
		path := base + "/" + iid + "/tool-context"
		if in.EndpointID != "" {
			eid, err := id("endpointId", in.EndpointID)
			if err != nil {
				return nil, err
			}
			path = base + "/" + iid + "/endpoints/" + eid + "/tool-context"
		}
		bindings := in.RequestBindings
		if bindings == nil {
			bindings = []requestBinding{}
		}
		return r.api.Do(ctx, http.MethodPut, path, nil, map[string]any{
			"requestBindings": bindings,
			"aiInstructions":  in.AIInstructions,
		})
	})
	add(r, write, needIntegrations, &mcp.Tool{
		Name:        "set_integration_active",
		Description: "Turn an integration on or off. Off hides all its tools from the assistant; nothing is deleted.",
	}, func(ctx context.Context, in setIntegrationActiveInput) (any, error) {
		iid, err := id("integrationId", in.IntegrationID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodPatch, base+"/"+iid+"/active", nil, map[string]bool{"isActive": in.IsActive})
	})
	add(r, write, needIntegrations, &mcp.Tool{
		Name: "test_integration_endpoint",
		Description: "Call one endpoint of the customer's real API, with the saved credentials, and show the status and response. " +
			"Anything but GET can change the customer's data, so it needs confirm=true after the user agreed.",
	}, func(ctx context.Context, in testEndpointInput) (any, error) {
		iid, err := id("integrationId", in.IntegrationID)
		if err != nil {
			return nil, err
		}
		integration, err := findIntegration(ctx, r.api, iid)
		if err != nil {
			return nil, err
		}
		var endpoint *endpointInfo
		for i := range integration.Endpoints {
			if integration.Endpoints[i].OperationID == in.OperationID {
				endpoint = &integration.Endpoints[i]
			}
		}
		if endpoint == nil {
			return nil, fmt.Errorf("integration %s has no operation %q", integration.Name, in.OperationID)
		}
		if !strings.EqualFold(endpoint.Method, http.MethodGet) && !in.Confirm {
			return nil, fmt.Errorf("%s %s may change data on the customer's system; ask the user, then call again with confirm=true", endpoint.Method, endpoint.Path)
		}
		useSaved := true
		if in.UseSavedCredentials != nil {
			useSaved = *in.UseSavedCredentials
		}
		return r.api.Do(ctx, http.MethodPost, base+"/"+iid+"/test", nil, map[string]any{
			"operationId":         in.OperationID,
			"pathParameters":      in.PathParameters,
			"queryParameters":     in.QueryParameters,
			"headers":             in.Headers,
			"body":                in.Body,
			"useSavedCredentials": useSaved,
		})
	})
	add(r, destructive, needIntegrations, &mcp.Tool{
		Name:        "delete_integration",
		Description: "Delete an integration, its endpoints and saved credentials; the assistant loses those tools. Prefer set_integration_active false. Needs confirm=true.",
	}, func(ctx context.Context, in deleteIntegrationInput) (any, error) {
		if err := requireConfirm(in.Confirm); err != nil {
			return nil, err
		}
		iid, err := id("integrationId", in.IntegrationID)
		if err != nil {
			return nil, err
		}
		return r.api.Do(ctx, http.MethodDelete, base+"/"+iid, nil, nil)
	})
}
