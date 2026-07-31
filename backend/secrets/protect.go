package secrets

import (
	"fmt"
	"sort"
	"strings"

	"apirequest/backend/model"
)

var sensitiveAuthParams = map[string]map[string]bool{
	"basic":  {"password": true},
	"digest": {"password": true},
	"bearer": {"token": true},
	"apikey": {"value": true},
	"oauth1": {
		"consumersecret": true,
		"token":          true,
		"tokensecret":    true,
	},
	"oauth2": {
		"clientsecret": true,
		"password":     true,
		"accesstoken":  true,
		"refreshtoken": true,
	},
	"awsv4": {
		"secretkey":    true,
		"sessiontoken": true,
	},
}

func normalizedKey(value string) string {
	replacer := strings.NewReplacer("_", "", "-", "", " ", "")
	return strings.ToLower(replacer.Replace(value))
}

func isSensitiveAuthParam(authType, key string) bool {
	params := sensitiveAuthParams[strings.ToLower(authType)]
	return params[normalizedKey(key)]
}

// ProtectAuth returns a copy whose sensitive values are persisted as Vault references.
func ProtectAuth(v SecretWriter, auth model.Auth, logicalPrefix string) (model.Auth, error) {
	out := model.Auth{Type: auth.Type}
	if auth.Params == nil {
		return out, nil
	}
	out.Params = make(map[string]string, len(auth.Params))
	keys := make([]string, 0, len(auth.Params))
	for key := range auth.Params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := auth.Params[key]
		if isSensitiveAuthParam(auth.Type, key) && value != "" && value != redactedText {
			ref, err := v.Put(logicalPrefix+"/auth/"+normalizedKey(key), value)
			if err != nil {
				return model.Auth{}, fmt.Errorf("protect %s auth parameter %s: %w", auth.Type, key, err)
			}
			value = ref
		}
		out.Params[key] = value
	}
	return out, nil
}

// ResolveAuth expands Vault references while keeping non-sensitive parameters unchanged.
func ResolveAuth(v *Vault, auth model.Auth) (model.Auth, error) {
	out := model.Auth{Type: auth.Type}
	if auth.Params == nil {
		return out, nil
	}
	out.Params = make(map[string]string, len(auth.Params))
	for key, value := range auth.Params {
		if isSensitiveAuthParam(auth.Type, key) && IsRef(value) {
			resolved, err := v.Resolve(value)
			if err != nil {
				return model.Auth{}, fmt.Errorf("resolve %s auth parameter %s: %w", auth.Type, key, err)
			}
			value = resolved
		}
		out.Params[key] = value
	}
	return out, nil
}

// ProtectVariables returns a copy with secret Variable values persisted as references.
func ProtectVariables(v SecretWriter, variables []model.Variable, logicalPrefix string) ([]model.Variable, error) {
	out := append([]model.Variable(nil), variables...)
	occurrences := map[string]int{}
	for i := range out {
		occurrence := occurrences[out[i].Key]
		occurrences[out[i].Key] = occurrence + 1
		if strings.EqualFold(out[i].Type, "secret") && out[i].Value != "" && out[i].Value != redactedText {
			ref, err := v.Put(logicalPrefix+"/variable/"+variableIdentity(out[i].Key, occurrence), out[i].Value)
			if err != nil {
				return nil, fmt.Errorf("protect variable %s: %w", out[i].Key, err)
			}
			out[i].Value = ref
		}
	}
	return out, nil
}

// variableIdentity distinguishes duplicate keys while remaining stable for an unchanged list order.
func variableIdentity(key string, occurrence int) string {
	return fmt.Sprintf("%d:%s/%d", len(key), key, occurrence)
}

// ResolveVariables expands Vault references in a copy of variables.
func ResolveVariables(v *Vault, variables []model.Variable) ([]model.Variable, error) {
	out := append([]model.Variable(nil), variables...)
	for i := range out {
		if strings.EqualFold(out[i].Type, "secret") && IsRef(out[i].Value) {
			resolved, err := v.Resolve(out[i].Value)
			if err != nil {
				return nil, fmt.Errorf("resolve variable %s: %w", out[i].Key, err)
			}
			out[i].Value = resolved
		}
	}
	return out, nil
}

// ProtectRequest persists request-level auth credentials and returns a copy.
func ProtectRequest(v SecretWriter, request model.HttpRequest, logicalPrefix string) (model.HttpRequest, error) {
	auth, err := ProtectAuth(v, request.Auth, logicalPrefix)
	if err != nil {
		return model.HttpRequest{}, err
	}
	request.Auth = auth
	return request, nil
}

// AuthReferences returns only references stored in credential-bearing auth
// parameters. Other auth fields are intentionally ignored because they are
// public configuration, not Vault-owned values.
func AuthReferences(auth model.Auth) []string {
	refs := make([]string, 0)
	for key, value := range auth.Params {
		if isSensitiveAuthParam(auth.Type, key) && IsRef(value) {
			refs = append(refs, value)
		}
	}
	return refs
}

// RequestReferences returns request-level auth references without scanning
// arbitrary request JSON such as URLs, bodies, scripts, or headers.
func RequestReferences(request model.HttpRequest) []string {
	return AuthReferences(request.Auth)
}

// VariableReferences returns references for typed secret variables only.
func VariableReferences(variables []model.Variable) []string {
	refs := make([]string, 0)
	for _, variable := range variables {
		if strings.EqualFold(variable.Type, "secret") && IsRef(variable.Value) {
			refs = append(refs, variable.Value)
		}
	}
	return refs
}

// NodeReferences returns all Vault references owned by a node's typed
// credential fields. It deliberately does not recurse through arbitrary JSON.
func NodeReferences(node model.Node) []string {
	refs := make([]string, 0)
	if node.Request != nil {
		refs = append(refs, RequestReferences(*node.Request)...)
	}
	if node.Auth != nil {
		refs = append(refs, AuthReferences(*node.Auth)...)
	}
	refs = append(refs, VariableReferences(node.Variables)...)
	return refs
}

// ResolveRequest expands request-level auth references in a copy.
func ResolveRequest(v *Vault, request model.HttpRequest) (model.HttpRequest, error) {
	auth, err := ResolveAuth(v, request.Auth)
	if err != nil {
		return model.HttpRequest{}, err
	}
	request.Auth = auth
	return request, nil
}

// RedactAuth irreversibly removes sensitive values for audit and export boundaries.
func RedactAuth(auth model.Auth) model.Auth {
	return transformAuthSecrets(auth, redactedText)
}

// OmitAuth clears sensitive values while preserving keys for sync merge restoration.
func OmitAuth(auth model.Auth) model.Auth {
	return transformAuthSecrets(auth, "")
}

func transformAuthSecrets(auth model.Auth, replacement string) model.Auth {
	out := model.Auth{Type: auth.Type}
	if auth.Params == nil {
		return out
	}
	out.Params = make(map[string]string, len(auth.Params))
	for key, value := range auth.Params {
		if isSensitiveAuthParam(auth.Type, key) && value != "" {
			value = replacement
		}
		out.Params[key] = value
	}
	return out
}

// RedactRequest irreversibly removes request-level auth credentials.
func RedactRequest(request model.HttpRequest) model.HttpRequest {
	request.Auth = RedactAuth(request.Auth)
	return request
}

// RedactNode removes auth credentials and secret variable values from a copy.
func RedactNode(node model.Node) model.Node {
	if node.Request != nil {
		request := RedactRequest(*node.Request)
		node.Request = &request
	}
	if node.Auth != nil {
		auth := RedactAuth(*node.Auth)
		node.Auth = &auth
	}
	node.Variables = RedactVariables(node.Variables)
	return node
}

// OmitNodeSecrets clears all Node credentials for a secret-omitting sync snapshot.
func OmitNodeSecrets(node model.Node) model.Node {
	if node.Request != nil {
		request := *node.Request
		request.Auth = OmitAuth(request.Auth)
		node.Request = &request
	}
	if node.Auth != nil {
		auth := OmitAuth(*node.Auth)
		node.Auth = &auth
	}
	node.Variables = OmitVariables(node.Variables)
	return node
}

// OmitVariables clears secret values while preserving row identity and metadata.
func OmitVariables(variables []model.Variable) []model.Variable {
	out := append([]model.Variable(nil), variables...)
	for i := range out {
		if strings.EqualFold(out[i].Type, "secret") {
			out[i].Value = ""
		}
	}
	return out
}

// RestoreOmittedNodeSecrets fills empty secret placeholders from a local Node copy.
func RestoreOmittedNodeSecrets(target *model.Node, local model.Node) {
	if target == nil {
		return
	}
	if target.Request != nil && local.Request != nil {
		target.Request.Auth = restoreAuth(target.Request.Auth, local.Request.Auth)
	}
	if target.Auth != nil && local.Auth != nil {
		restored := restoreAuth(*target.Auth, *local.Auth)
		target.Auth = &restored
	}
	target.Variables = RestoreOmittedVariables(target.Variables, local.Variables)
}

// RestoreOmittedVariables matches duplicate keys by their occurrence within the list.
func RestoreOmittedVariables(target, local []model.Variable) []model.Variable {
	out := append([]model.Variable(nil), target...)
	localVariables := map[string]string{}
	localOccurrences := map[string]int{}
	for _, variable := range local {
		occurrence := localOccurrences[variable.Key]
		localOccurrences[variable.Key] = occurrence + 1
		if strings.EqualFold(variable.Type, "secret") && variable.Value != "" {
			localVariables[variableIdentity(variable.Key, occurrence)] = variable.Value
		}
	}
	targetOccurrences := map[string]int{}
	for i := range out {
		variable := &out[i]
		occurrence := targetOccurrences[variable.Key]
		targetOccurrences[variable.Key] = occurrence + 1
		if strings.EqualFold(variable.Type, "secret") && variable.Value == "" {
			variable.Value = localVariables[variableIdentity(variable.Key, occurrence)]
		}
	}
	return out
}

func restoreAuth(target, local model.Auth) model.Auth {
	if !strings.EqualFold(target.Type, local.Type) || target.Params == nil {
		return target
	}
	for key, value := range target.Params {
		if value == "" && isSensitiveAuthParam(target.Type, key) {
			if localValue, ok := local.Params[key]; ok {
				target.Params[key] = localValue
			}
		}
	}
	return target
}

// RedactVariables preserves keys and metadata while removing secret values.
func RedactVariables(variables []model.Variable) []model.Variable {
	out := append([]model.Variable(nil), variables...)
	for i := range out {
		if strings.EqualFold(out[i].Type, "secret") && out[i].Value != "" {
			out[i].Value = redactedText
		}
	}
	return out
}

// NewRedactor collects explicit credential values in addition to Vault-observed values.
func NewRedactor(v *Vault, values ...string) *Redactor {
	r := &Redactor{vault: v, values: map[string]struct{}{}}
	for _, value := range values {
		r.Add(value)
	}
	return r
}

// Redactor scrubs arbitrary log text without exposing the Vault's internal cache.
type Redactor struct {
	vault  *Vault
	values map[string]struct{}
}

// Add registers a plaintext credential for the lifetime of this Redactor.
func (r *Redactor) Add(value string) {
	if value != "" && value != redactedText {
		r.values[value] = struct{}{}
	}
}

// String scrubs registered and Vault-observed credential values.
func (r *Redactor) String(input string) string {
	input = redactKnown(input, r.values)
	if r.vault != nil {
		input = r.vault.RedactString(input)
	}
	return input
}

// Strings scrubs a copy of a string slice.
func (r *Redactor) Strings(input []string) []string {
	out := append([]string(nil), input...)
	for i := range out {
		out[i] = r.String(out[i])
	}
	return out
}

// Request scrubs every user-controlled string field and irreversibly redacts auth credentials.
func (r *Redactor) Request(request model.HttpRequest) model.HttpRequest {
	request.Method = r.String(request.Method)
	request.Url = r.String(request.Url)
	request.Params = r.kvs(request.Params)
	request.Headers = r.kvs(request.Headers)
	request.Body.Kind = r.String(request.Body.Kind)
	request.Body.Language = r.String(request.Body.Language)
	request.Body.Text = r.String(request.Body.Text)
	request.Body.Path = r.String(request.Body.Path)
	request.Body.Query = r.String(request.Body.Query)
	request.Body.Variables = r.String(request.Body.Variables)
	request.Body.Items = append([]model.FormItem(nil), request.Body.Items...)
	for i := range request.Body.Items {
		item := &request.Body.Items[i]
		item.Key = r.String(item.Key)
		item.Type = r.String(item.Type)
		item.Value = r.String(item.Value)
		item.Path = r.String(item.Path)
	}
	request.Auth = RedactAuth(request.Auth)
	request.Auth.Type = r.String(request.Auth.Type)
	for key, value := range request.Auth.Params {
		request.Auth.Params[key] = r.String(value)
	}
	request.PreScript = r.String(request.PreScript)
	request.TestScript = r.String(request.TestScript)
	return request
}

func (r *Redactor) kvs(input []model.KV) []model.KV {
	out := append([]model.KV(nil), input...)
	for i := range out {
		out[i].Key = r.String(out[i].Key)
		out[i].Value = r.String(out[i].Value)
		out[i].Description = r.String(out[i].Description)
	}
	return out
}

// AuthValues returns sensitive plaintext values for request-scoped log redaction.
func AuthValues(auth model.Auth) []string {
	values := []string{}
	for key, value := range auth.Params {
		if isSensitiveAuthParam(auth.Type, key) && value != "" && !IsRef(value) {
			values = append(values, value)
		}
	}
	return values
}
