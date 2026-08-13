package secrets

import (
	"errors"
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

// IsSensitiveRequestValueKey centralises the structured request credential
// policy used by headers, query parameters and form values.
func IsSensitiveRequestValueKey(key string) bool {
	normalized := normalizedKey(strings.TrimSpace(key))
	return strings.Contains(normalized, "authorization") ||
		strings.Contains(normalized, "cookie") ||
		strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "apikey") ||
		strings.Contains(normalized, "password") ||
		strings.Contains(normalized, "passwd")
}

// IsSensitiveHeader remains the header-specific public policy seam.
func IsSensitiveHeader(key string) bool { return IsSensitiveRequestValueKey(key) }

func normalizedHeaderKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}

func headerIdentity(key string, occurrence int) string {
	key = normalizedHeaderKey(key)
	return fmt.Sprintf("%d:%s/%d", len(key), key, occurrence)
}

func protectKVs(v SecretWriter, values []model.KV, logicalPrefix, segment string) ([]model.KV, error) {
	out := append([]model.KV(nil), values...)
	occurrences := map[string]int{}
	for i := range out {
		key := normalizedHeaderKey(out[i].Key)
		occurrence := occurrences[key]
		occurrences[key] = occurrence + 1
		if IsSensitiveRequestValueKey(out[i].Key) && out[i].Value != "" && out[i].Value != redactedText {
			ref, err := v.Put(logicalPrefix+"/"+segment+"/"+headerIdentity(out[i].Key, occurrence), out[i].Value)
			if err != nil {
				return nil, fmt.Errorf("protect request %s %s: %w", segment, out[i].Key, err)
			}
			out[i].Value = ref
		}
	}
	return out, nil
}

func resolveKVs(v *Vault, values []model.KV, field string) ([]model.KV, error) {
	out := append([]model.KV(nil), values...)
	for i := range out {
		if IsSensitiveRequestValueKey(out[i].Key) && IsRef(out[i].Value) {
			value, err := v.Resolve(out[i].Value)
			if err != nil {
				return nil, fmt.Errorf("resolve request %s %s: %w", field, out[i].Key, err)
			}
			out[i].Value = value
		}
	}
	return out, nil
}

func protectFormItems(v SecretWriter, items []model.FormItem, logicalPrefix string) ([]model.FormItem, error) {
	out := append([]model.FormItem(nil), items...)
	occurrences := map[string]int{}
	for i := range out {
		key := normalizedHeaderKey(out[i].Key)
		occurrence := occurrences[key]
		occurrences[key] = occurrence + 1
		if !strings.EqualFold(out[i].Type, "file") && IsSensitiveRequestValueKey(out[i].Key) && out[i].Value != "" && out[i].Value != redactedText {
			ref, err := v.Put(logicalPrefix+"/form/"+headerIdentity(out[i].Key, occurrence), out[i].Value)
			if err != nil {
				return nil, fmt.Errorf("protect request form value %s: %w", out[i].Key, err)
			}
			out[i].Value = ref
		}
	}
	return out, nil
}

func resolveFormItems(v *Vault, items []model.FormItem) ([]model.FormItem, error) {
	out := append([]model.FormItem(nil), items...)
	for i := range out {
		if !strings.EqualFold(out[i].Type, "file") && IsSensitiveRequestValueKey(out[i].Key) && IsRef(out[i].Value) {
			value, err := v.Resolve(out[i].Value)
			if err != nil {
				return nil, fmt.Errorf("resolve request form value %s: %w", out[i].Key, err)
			}
			out[i].Value = value
		}
	}
	return out, nil
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

// ProtectHeaders persists sensitive request-header values. Disabled rows are
// protected as well because they remain editable persisted credentials.
func ProtectHeaders(v SecretWriter, headers []model.KV, logicalPrefix string) ([]model.KV, error) {
	out := append([]model.KV(nil), headers...)
	occurrences := map[string]int{}
	for i := range out {
		key := normalizedHeaderKey(out[i].Key)
		occurrence := occurrences[key]
		occurrences[key] = occurrence + 1
		if IsSensitiveHeader(out[i].Key) && out[i].Value != "" && out[i].Value != redactedText {
			ref, err := v.Put(logicalPrefix+"/header/"+headerIdentity(out[i].Key, occurrence), out[i].Value)
			if err != nil {
				return nil, fmt.Errorf("protect request header %s: %w", out[i].Key, err)
			}
			out[i].Value = ref
		}
	}
	return out, nil
}

// ResolveHeaders expands sensitive header references in a copy.
func ResolveHeaders(v *Vault, headers []model.KV) ([]model.KV, error) {
	out := append([]model.KV(nil), headers...)
	for i := range out {
		if IsSensitiveHeader(out[i].Key) && IsRef(out[i].Value) {
			value, err := v.Resolve(out[i].Value)
			if err != nil {
				return nil, fmt.Errorf("resolve request header %s: %w", out[i].Key, err)
			}
			out[i].Value = value
		}
	}
	return out, nil
}

// ProtectRequest persists request-level auth and header credentials and returns a copy.
func ProtectRequest(v SecretWriter, request model.HttpRequest, logicalPrefix string) (model.HttpRequest, error) {
	auth, err := ProtectAuth(v, request.Auth, logicalPrefix)
	if err != nil {
		return model.HttpRequest{}, err
	}
	headers, err := ProtectHeaders(v, request.Headers, logicalPrefix)
	if err != nil {
		return model.HttpRequest{}, err
	}
	params, err := protectKVs(v, request.Params, logicalPrefix, "query")
	if err != nil {
		return model.HttpRequest{}, err
	}
	items, err := protectFormItems(v, request.Body.Items, logicalPrefix)
	if err != nil {
		return model.HttpRequest{}, err
	}
	request.Auth = auth
	request.Headers = headers
	request.Params = params
	request.Body.Items = items
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

// RequestReferences returns references from explicit structured credential
// fields. Raw URLs, body text and scripts are intentionally not scanned.
func RequestReferences(request model.HttpRequest) []string {
	refs := AuthReferences(request.Auth)
	for _, header := range request.Headers {
		if IsSensitiveHeader(header.Key) && IsRef(header.Value) {
			refs = append(refs, header.Value)
		}
	}
	for _, param := range request.Params {
		if IsSensitiveRequestValueKey(param.Key) && IsRef(param.Value) {
			refs = append(refs, param.Value)
		}
	}
	for _, item := range request.Body.Items {
		if !strings.EqualFold(item.Type, "file") && IsSensitiveRequestValueKey(item.Key) && IsRef(item.Value) {
			refs = append(refs, item.Value)
		}
	}
	return refs
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

// ResolveRequest expands request-level auth and header references in a copy.
func ResolveRequest(v *Vault, request model.HttpRequest) (model.HttpRequest, error) {
	auth, err := ResolveAuth(v, request.Auth)
	if err != nil {
		return model.HttpRequest{}, err
	}
	headers, err := ResolveHeaders(v, request.Headers)
	if err != nil {
		return model.HttpRequest{}, err
	}
	params, err := resolveKVs(v, request.Params, "query parameter")
	if err != nil {
		return model.HttpRequest{}, err
	}
	items, err := resolveFormItems(v, request.Body.Items)
	if err != nil {
		return model.HttpRequest{}, err
	}
	request.Auth = auth
	request.Headers = headers
	request.Params = params
	request.Body.Items = items
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
	request.Headers = transformHeaderSecrets(request.Headers, redactedText)
	request.Params = transformKVSecrets(request.Params, redactedText)
	request.Body.Items = transformFormItemSecrets(request.Body.Items, redactedText)
	return request
}

func transformKVSecrets(values []model.KV, replacement string) []model.KV {
	out := append([]model.KV(nil), values...)
	for i := range out {
		if IsSensitiveRequestValueKey(out[i].Key) && out[i].Value != "" {
			out[i].Value = replacement
		}
	}
	return out
}

func transformFormItemSecrets(items []model.FormItem, replacement string) []model.FormItem {
	out := append([]model.FormItem(nil), items...)
	for i := range out {
		if !strings.EqualFold(out[i].Type, "file") && IsSensitiveRequestValueKey(out[i].Key) && out[i].Value != "" {
			out[i].Value = replacement
		}
	}
	return out
}

func transformHeaderSecrets(headers []model.KV, replacement string) []model.KV {
	out := append([]model.KV(nil), headers...)
	for i := range out {
		if IsSensitiveHeader(out[i].Key) && out[i].Value != "" {
			out[i].Value = replacement
		}
	}
	return out
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
		request.Headers = transformHeaderSecrets(request.Headers, "")
		request.Params = transformKVSecrets(request.Params, "")
		request.Body.Items = transformFormItemSecrets(request.Body.Items, "")
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
		target.Request.Headers = restoreHeaders(target.Request.Headers, local.Request.Headers)
		target.Request.Params = restoreKVs(target.Request.Params, local.Request.Params)
		target.Request.Body.Items = restoreFormItems(target.Request.Body.Items, local.Request.Body.Items)
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

func restoreHeaders(target, local []model.KV) []model.KV {
	out := append([]model.KV(nil), target...)
	localValues := map[string]string{}
	localOccurrences := map[string]int{}
	for _, header := range local {
		key := normalizedHeaderKey(header.Key)
		occurrence := localOccurrences[key]
		localOccurrences[key] = occurrence + 1
		if IsSensitiveHeader(header.Key) && header.Value != "" {
			localValues[headerIdentity(header.Key, occurrence)] = header.Value
		}
	}
	targetOccurrences := map[string]int{}
	for i := range out {
		key := normalizedHeaderKey(out[i].Key)
		occurrence := targetOccurrences[key]
		targetOccurrences[key] = occurrence + 1
		if IsSensitiveHeader(out[i].Key) && out[i].Value == "" {
			out[i].Value = localValues[headerIdentity(out[i].Key, occurrence)]
		}
	}
	return out
}

func restoreKVs(target, local []model.KV) []model.KV {
	out := append([]model.KV(nil), target...)
	localValues := map[string]string{}
	localOccurrences := map[string]int{}
	for _, value := range local {
		key := normalizedHeaderKey(value.Key)
		occurrence := localOccurrences[key]
		localOccurrences[key] = occurrence + 1
		if IsSensitiveRequestValueKey(value.Key) && value.Value != "" {
			localValues[headerIdentity(value.Key, occurrence)] = value.Value
		}
	}
	targetOccurrences := map[string]int{}
	for i := range out {
		key := normalizedHeaderKey(out[i].Key)
		occurrence := targetOccurrences[key]
		targetOccurrences[key] = occurrence + 1
		if IsSensitiveRequestValueKey(out[i].Key) && out[i].Value == "" {
			out[i].Value = localValues[headerIdentity(out[i].Key, occurrence)]
		}
	}
	return out
}

func restoreFormItems(target, local []model.FormItem) []model.FormItem {
	out := append([]model.FormItem(nil), target...)
	localValues := map[string]string{}
	localOccurrences := map[string]int{}
	for _, item := range local {
		key := normalizedHeaderKey(item.Key)
		occurrence := localOccurrences[key]
		localOccurrences[key] = occurrence + 1
		if !strings.EqualFold(item.Type, "file") && IsSensitiveRequestValueKey(item.Key) && item.Value != "" {
			localValues[headerIdentity(item.Key, occurrence)] = item.Value
		}
	}
	targetOccurrences := map[string]int{}
	for i := range out {
		key := normalizedHeaderKey(out[i].Key)
		occurrence := targetOccurrences[key]
		targetOccurrences[key] = occurrence + 1
		if !strings.EqualFold(out[i].Type, "file") && IsSensitiveRequestValueKey(out[i].Key) && out[i].Value == "" {
			out[i].Value = localValues[headerIdentity(out[i].Key, occurrence)]
		}
	}
	return out
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
	values := make(map[string]struct{}, len(r.values))
	for value := range r.values {
		values[value] = struct{}{}
	}
	if r.vault != nil {
		for value := range r.vault.knownValuesSnapshot() {
			values[value] = struct{}{}
		}
	}
	return redactKnown(input, values)
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
	request.Params = r.requestKVs(request.Params)
	request.Headers = r.requestHeaders(request.Headers)
	request.Body.Kind = r.String(request.Body.Kind)
	request.Body.Language = r.String(request.Body.Language)
	request.Body.Text = r.String(request.Body.Text)
	request.Body.Path = r.String(request.Body.Path)
	request.Body.Query = r.String(request.Body.Query)
	request.Body.Variables = r.String(request.Body.Variables)
	request.Body.Items = append([]model.FormItem(nil), request.Body.Items...)
	for i := range request.Body.Items {
		item := &request.Body.Items[i]
		sensitive := !strings.EqualFold(item.Type, "file") && IsSensitiveRequestValueKey(item.Key)
		item.Key = r.String(item.Key)
		item.Type = r.String(item.Type)
		if sensitive && item.Value != "" {
			item.Value = redactedText
		} else {
			item.Value = r.String(item.Value)
		}
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

func (r *Redactor) requestHeaders(input []model.KV) []model.KV {
	out := append([]model.KV(nil), input...)
	for i := range out {
		sensitive := IsSensitiveHeader(out[i].Key)
		out[i].Key = r.String(out[i].Key)
		out[i].Description = r.String(out[i].Description)
		if sensitive && out[i].Value != "" {
			out[i].Value = redactedText
		} else {
			out[i].Value = r.String(out[i].Value)
		}
	}
	return out
}

func (r *Redactor) requestKVs(input []model.KV) []model.KV {
	out := append([]model.KV(nil), input...)
	for i := range out {
		sensitive := IsSensitiveRequestValueKey(out[i].Key)
		out[i].Key = r.String(out[i].Key)
		out[i].Description = r.String(out[i].Description)
		if sensitive && out[i].Value != "" {
			out[i].Value = redactedText
		} else {
			out[i].Value = r.String(out[i].Value)
		}
	}
	return out
}

// ResponseHeaders scrubs reflected secrets and always removes credential-bearing
// response header values from audit data.
func (r *Redactor) ResponseHeaders(input []model.KV) []model.KV {
	out := append([]model.KV(nil), input...)
	for i := range out {
		sensitive := IsSensitiveHeader(out[i].Key)
		out[i].Key = r.String(out[i].Key)
		out[i].Description = r.String(out[i].Description)
		if sensitive && out[i].Value != "" {
			out[i].Value = redactedText
		} else {
			out[i].Value = r.String(out[i].Value)
		}
	}
	return out
}

// TestResults scrubs assertion text before it crosses the history audit seam.
func (r *Redactor) TestResults(input []model.TestResult) []model.TestResult {
	out := append([]model.TestResult(nil), input...)
	for i := range out {
		out[i].Name = r.String(out[i].Name)
		out[i].Error = r.String(out[i].Error)
	}
	return out
}

// AuthValues returns sensitive values supplied at a public request boundary.
// Reference-looking text is included because users may intentionally send it.
func AuthValues(auth model.Auth) []string {
	values := []string{}
	for key, value := range auth.Params {
		if isSensitiveAuthParam(auth.Type, key) && value != "" && value != redactedText {
			values = append(values, value)
		}
	}
	return values
}

// HeaderValues returns sensitive plaintext request-header values for
// request-scoped log, response and history redaction.
func HeaderValues(headers []model.KV) []string {
	values := []string{}
	for _, header := range headers {
		value := strings.TrimSpace(header.Value)
		if !IsSensitiveHeader(header.Key) || value == "" || value == redactedText {
			continue
		}
		values = append(values, value)
		key := normalizedHeaderKey(header.Key)
		if strings.Contains(key, "authorization") {
			if _, credential, found := strings.Cut(value, " "); found && strings.TrimSpace(credential) != "" {
				values = append(values, strings.TrimSpace(credential))
			}
		}
		if strings.Contains(key, "cookie") {
			for _, part := range strings.Split(value, ";") {
				if _, cookieValue, found := strings.Cut(strings.TrimSpace(part), "="); found && strings.TrimSpace(cookieValue) != "" {
					values = append(values, strings.TrimSpace(cookieValue))
				}
			}
		}
	}
	return values
}

// RequestCredentialValues returns every plaintext credential from structured
// request fields so echoed values are scrubbed from logs and audit payloads.
func RequestCredentialValues(request model.HttpRequest) []string {
	values := append([]string{}, AuthValues(request.Auth)...)
	values = append(values, HeaderValues(request.Headers)...)
	for _, param := range request.Params {
		if IsSensitiveRequestValueKey(param.Key) && param.Value != "" && param.Value != redactedText {
			values = append(values, param.Value)
		}
	}
	for _, item := range request.Body.Items {
		if !strings.EqualFold(item.Type, "file") && IsSensitiveRequestValueKey(item.Key) && item.Value != "" && item.Value != redactedText {
			values = append(values, item.Value)
		}
	}
	return values
}

// StoredRequestCredentialValues resolves genuine Vault references before
// collecting values from persisted request JSON. Missing references are
// treated as literal legacy input; locked references defer the caller so an
// audit migration does not destroy the only remaining link to the plaintext.
func StoredRequestCredentialValues(v *Vault, request model.HttpRequest) ([]string, error) {
	resolve := func(value string) (string, error) {
		if value == "" || value == redactedText || !IsRef(value) {
			return value, nil
		}
		resolved, err := v.Resolve(value)
		if err == nil {
			return resolved, nil
		}
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalidRef) {
			return value, nil
		}
		return "", err
	}

	stored := request
	if stored.Auth.Params != nil {
		stored.Auth.Params = make(map[string]string, len(request.Auth.Params))
		for key, value := range request.Auth.Params {
			if isSensitiveAuthParam(request.Auth.Type, key) {
				resolved, err := resolve(value)
				if err != nil {
					return nil, err
				}
				value = resolved
			}
			stored.Auth.Params[key] = value
		}
	}
	stored.Headers = append([]model.KV(nil), request.Headers...)
	for i := range stored.Headers {
		if !IsSensitiveHeader(stored.Headers[i].Key) {
			continue
		}
		resolved, err := resolve(stored.Headers[i].Value)
		if err != nil {
			return nil, err
		}
		stored.Headers[i].Value = resolved
	}
	stored.Params = append([]model.KV(nil), request.Params...)
	for i := range stored.Params {
		if !IsSensitiveRequestValueKey(stored.Params[i].Key) {
			continue
		}
		resolved, err := resolve(stored.Params[i].Value)
		if err != nil {
			return nil, err
		}
		stored.Params[i].Value = resolved
	}
	stored.Body.Items = append([]model.FormItem(nil), request.Body.Items...)
	for i := range stored.Body.Items {
		item := &stored.Body.Items[i]
		if strings.EqualFold(item.Type, "file") || !IsSensitiveRequestValueKey(item.Key) {
			continue
		}
		resolved, err := resolve(item.Value)
		if err != nil {
			return nil, err
		}
		item.Value = resolved
	}
	return RequestCredentialValues(stored), nil
}
