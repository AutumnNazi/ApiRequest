package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"apirequest/backend/model"
	"apirequest/backend/secrets"
)

const (
	secretMigrationKey       = "secrets.migration.v1"
	cookieSecretMigrationKey = "secrets.cookie.migration.v1"
)

type plaintextSecretWriter struct{ secrets.SecretWriter }

func (w plaintextSecretWriter) Put(logicalKey, value string) (string, error) {
	return w.SecretWriter.PutPlaintext(logicalKey, value)
}

func protectNode(writer secrets.SecretWriter, node model.Node) (model.Node, error) {
	writer = plaintextSecretWriter{SecretWriter: writer}
	if node.Request != nil {
		request, err := secrets.ProtectRequest(writer, *node.Request, "node/"+node.Id+"/request")
		if err != nil {
			return model.Node{}, err
		}
		node.Request = &request
	}
	if node.Auth != nil {
		auth, err := secrets.ProtectAuth(writer, *node.Auth, "node/"+node.Id)
		if err != nil {
			return model.Node{}, err
		}
		node.Auth = &auth
	}
	variables, err := secrets.ProtectVariables(writer, node.Variables, "node/"+node.Id)
	if err != nil {
		return model.Node{}, err
	}
	node.Variables = variables
	return node, nil
}

func protectVariables(writer secrets.SecretWriter, variables []model.Variable, logicalPrefix string) ([]model.Variable, error) {
	return secrets.ProtectVariables(plaintextSecretWriter{SecretWriter: writer}, variables, logicalPrefix)
}

func (s *Store) withSecretWrite(write func(secrets.SecretWriter) error) error {
	s.secretWriteMu.Lock()
	defer s.secretWriteMu.Unlock()
	batch := s.vault.BeginWrite()
	if err := write(batch); err != nil {
		if rollbackErr := batch.Rollback(); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("rollback secret vault: %w", rollbackErr))
		}
		return err
	}
	batch.Commit()
	return nil
}

// UpdateSecretSetting serializes one setting update with its recoverable Vault
// changes. The callback only builds the next payload; Store owns the DB write.
func (s *Store) UpdateSecretSetting(key string, update func(string, secrets.SecretWriter) (string, error)) error {
	if update == nil {
		return errors.New("secret setting update is required")
	}
	return s.withSecretWrite(func(writer secrets.SecretWriter) error {
		current, err := s.GetSetting(key)
		if err != nil {
			return err
		}
		next, err := update(current, writer)
		if err != nil {
			return err
		}
		return s.SetSetting(key, next)
	})
}

func addSecretReferenceCounts(counts map[string]int, refs ...string) {
	for _, ref := range refs {
		if secrets.IsRef(ref) {
			counts[ref]++
		}
	}
}

func appendStoredNodeReferences(counts map[string]int, requestData, authData, variablesData sql.NullString) error {
	if requestData.Valid && requestData.String != "" {
		var request model.HttpRequest
		if err := json.Unmarshal([]byte(requestData.String), &request); err != nil {
			return fmt.Errorf("decode stored request credentials: %w", err)
		}
		addSecretReferenceCounts(counts, secrets.RequestReferences(request)...)
	}
	if authData.Valid && authData.String != "" {
		var auth model.Auth
		if err := json.Unmarshal([]byte(authData.String), &auth); err != nil {
			return fmt.Errorf("decode stored auth credentials: %w", err)
		}
		addSecretReferenceCounts(counts, secrets.AuthReferences(auth)...)
	}
	if variablesData.Valid && variablesData.String != "" {
		var variables []model.Variable
		if err := json.Unmarshal([]byte(variablesData.String), &variables); err != nil {
			return fmt.Errorf("decode stored variable credentials: %w", err)
		}
		addSecretReferenceCounts(counts, secrets.VariableReferences(variables)...)
	}
	return nil
}

func (s *Store) storedNodeSecretReferences(id string) ([]string, error) {
	var requestData, authData, variablesData sql.NullString
	err := s.db.QueryRow("SELECT request_data, auth, variables FROM node WHERE id = ?", id).
		Scan(&requestData, &authData, &variablesData)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int)
	if err := appendStoredNodeReferences(counts, requestData, authData, variablesData); err != nil {
		return nil, err
	}
	return sortedSecretReferences(counts), nil
}

func (s *Store) storedEnvironmentSecretReferences(id string) ([]string, error) {
	var raw string
	err := s.db.QueryRow("SELECT variables FROM environment WHERE id = ?", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var variables []model.Variable
	if err := json.Unmarshal([]byte(raw), &variables); err != nil {
		return nil, fmt.Errorf("decode stored environment credentials: %w", err)
	}
	return secrets.VariableReferences(variables), nil
}

func (s *Store) storedGlobalSecretReferences(workspaceID string) ([]string, error) {
	var raw string
	err := s.db.QueryRow("SELECT variables FROM global_var WHERE workspace_id = ?", workspaceID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var variables []model.Variable
	if err := json.Unmarshal([]byte(raw), &variables); err != nil {
		return nil, fmt.Errorf("decode stored global credentials: %w", err)
	}
	return secrets.VariableReferences(variables), nil
}

func (s *Store) storedWorkspaceSecretReferences(workspaceID string) ([]string, error) {
	counts := make(map[string]int)
	rows, err := s.db.Query("SELECT request_data, auth, variables FROM node WHERE workspace_id = ?", workspaceID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var requestData, authData, variablesData sql.NullString
		if err := rows.Scan(&requestData, &authData, &variablesData); err != nil {
			rows.Close()
			return nil, err
		}
		if err := appendStoredNodeReferences(counts, requestData, authData, variablesData); err != nil {
			rows.Close()
			return nil, err
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	rows, err = s.db.Query("SELECT variables FROM environment WHERE workspace_id = ?", workspaceID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		var variables []model.Variable
		if err := json.Unmarshal([]byte(raw), &variables); err != nil {
			rows.Close()
			return nil, fmt.Errorf("decode stored environment credentials: %w", err)
		}
		addSecretReferenceCounts(counts, secrets.VariableReferences(variables)...)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	var raw string
	err = s.db.QueryRow("SELECT variables FROM global_var WHERE workspace_id = ?", workspaceID).Scan(&raw)
	if err == nil {
		var variables []model.Variable
		if err := json.Unmarshal([]byte(raw), &variables); err != nil {
			return nil, fmt.Errorf("decode stored global credentials: %w", err)
		}
		addSecretReferenceCounts(counts, secrets.VariableReferences(variables)...)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return sortedSecretReferences(counts), nil
}

func sortedSecretReferences(counts map[string]int) []string {
	total := 0
	for _, count := range counts {
		total += count
	}
	refs := make([]string, 0, total)
	for ref, count := range counts {
		for range count {
			refs = append(refs, ref)
		}
	}
	sort.Strings(refs)
	return refs
}

// deleteRemovedSecretReferences removes references no longer owned by the
// current entity. External writes are re-keyed through plaintextSecretWriter,
// so entity-scoped logical keys keep these references independent.
func deleteRemovedSecretReferences(writer secrets.SecretWriter, oldRefs, newRefs []string) error {
	newSet := make(map[string]struct{}, len(newRefs))
	for _, ref := range newRefs {
		newSet[ref] = struct{}{}
	}
	oldSet := make(map[string]struct{}, len(oldRefs))
	for _, ref := range oldRefs {
		oldSet[ref] = struct{}{}
	}
	refs := make([]string, 0, len(oldSet))
	for ref := range oldSet {
		if _, kept := newSet[ref]; !kept {
			refs = append(refs, ref)
		}
	}
	sort.Strings(refs)
	for _, ref := range refs {
		if err := writer.Delete(ref); err != nil {
			return err
		}
	}
	return nil
}

// MigrateSecrets replaces legacy plaintext credentials with Vault references in-place.
// It is idempotent and does not change entity timestamps.
func (s *Store) MigrateSecrets() error {
	if !s.vault.Status().CanStore {
		return secrets.ErrLocked
	}
	done, err := s.GetSetting(secretMigrationKey)
	if err != nil {
		return err
	}
	if done != "1" {
		if err := s.migrateNodeSecrets(); err != nil {
			return err
		}
		if err := s.migrateVariableSecrets("environment", "id", "variables", "environment/"); err != nil {
			return err
		}
		if err := s.migrateVariableSecrets("global_var", "workspace_id", "variables", "workspace/"); err != nil {
			return err
		}
		if err := s.migrateSyncPassword(); err != nil {
			return err
		}
		if err := s.SetSetting(secretMigrationKey, "1"); err != nil {
			return err
		}
	}
	cookieDone, err := s.GetSetting(cookieSecretMigrationKey)
	if err != nil || cookieDone == "1" {
		return err
	}
	if err := s.migrateCookieSecrets(); err != nil {
		return err
	}
	return s.SetSetting(cookieSecretMigrationKey, "1")
}

func (s *Store) migrateCookieSecrets() error {
	rows, err := s.db.Query("SELECT id, value FROM cookie")
	if err != nil {
		return err
	}
	type storedCookie struct{ id, value string }
	stored := []storedCookie{}
	for rows.Next() {
		var cookie storedCookie
		if err := rows.Scan(&cookie.id, &cookie.value); err != nil {
			rows.Close()
			return err
		}
		if cookie.value != "" && !secrets.IsRef(cookie.value) {
			stored = append(stored, cookie)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(stored) == 0 {
		return nil
	}
	return s.withSecretWrite(func(writer secrets.SecretWriter) error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, cookie := range stored {
			ref, err := writer.PutPlaintext(cookieSecretPrefix+cookie.id+"/value", cookie.value)
			if err != nil {
				return err
			}
			if _, err := tx.Exec("UPDATE cookie SET value = ? WHERE id = ?", ref, cookie.id); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
}

type storedNodeSecrets struct {
	id, requestData, auth, variables string
}

func (s *Store) migrateNodeSecrets() error {
	rows, err := s.db.Query("SELECT id, COALESCE(request_data,''), COALESCE(auth,''), COALESCE(variables,'') FROM node")
	if err != nil {
		return err
	}
	var stored []storedNodeSecrets
	for rows.Next() {
		var row storedNodeSecrets
		if err := rows.Scan(&row.id, &row.requestData, &row.auth, &row.variables); err != nil {
			rows.Close()
			return err
		}
		stored = append(stored, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, row := range stored {
		if err := s.withSecretWrite(func(writer secrets.SecretWriter) error {
			var request model.HttpRequest
			var auth model.Auth
			variables := []model.Variable{}
			if row.requestData != "" {
				if err := json.Unmarshal([]byte(row.requestData), &request); err != nil {
					return err
				}
				protected, err := secrets.ProtectRequest(writer, request, "node/"+row.id+"/request")
				if err != nil {
					return err
				}
				data, err := json.Marshal(protected)
				if err != nil {
					return err
				}
				row.requestData = string(data)
			}
			if row.auth != "" {
				if err := json.Unmarshal([]byte(row.auth), &auth); err != nil {
					return err
				}
				protected, err := secrets.ProtectAuth(writer, auth, "node/"+row.id)
				if err != nil {
					return err
				}
				data, err := json.Marshal(protected)
				if err != nil {
					return err
				}
				row.auth = string(data)
			}
			if row.variables != "" {
				if err := json.Unmarshal([]byte(row.variables), &variables); err != nil {
					return err
				}
				protected, err := secrets.ProtectVariables(writer, variables, "node/"+row.id)
				if err != nil {
					return err
				}
				data, err := json.Marshal(protected)
				if err != nil {
					return err
				}
				row.variables = string(data)
			}
			_, err := s.db.Exec("UPDATE node SET request_data = NULLIF(?,''), auth = NULLIF(?,''), variables = NULLIF(?,'') WHERE id = ?", row.requestData, row.auth, row.variables, row.id)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) migrateVariableSecrets(table, idColumn, valueColumn, prefix string) error {
	query := fmt.Sprintf("SELECT %s, %s FROM %s", idColumn, valueColumn, table)
	rows, err := s.db.Query(query)
	if err != nil {
		return err
	}
	type variableRow struct{ id, raw string }
	var stored []variableRow
	for rows.Next() {
		var row variableRow
		if err := rows.Scan(&row.id, &row.raw); err != nil {
			rows.Close()
			return err
		}
		stored = append(stored, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, row := range stored {
		if err := s.withSecretWrite(func(writer secrets.SecretWriter) error {
			variables := []model.Variable{}
			if err := json.Unmarshal([]byte(row.raw), &variables); err != nil {
				return err
			}
			logicalPrefix := prefix + row.id
			if table == "global_var" {
				logicalPrefix += "/globals"
			}
			protected, err := secrets.ProtectVariables(writer, variables, logicalPrefix)
			if err != nil {
				return err
			}
			data, err := json.Marshal(protected)
			if err != nil {
				return err
			}
			update := fmt.Sprintf("UPDATE %s SET %s = ? WHERE %s = ?", table, valueColumn, idColumn)
			_, err = s.db.Exec(update, string(data), row.id)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) migrateSyncPassword() error {
	raw, err := s.GetSetting("sync.webdav")
	if err != nil || raw == "" {
		return err
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return err
	}
	password, _ := cfg["password"].(string)
	if password == "" || secrets.IsRef(password) {
		return nil
	}
	return s.withSecretWrite(func(writer secrets.SecretWriter) error {
		ref, err := writer.PutPlaintext("setting/sync.webdav/password", password)
		if err != nil {
			return err
		}
		cfg["password"] = ref
		data, err := json.Marshal(cfg)
		if err == nil {
			_, err = s.db.Exec(`UPDATE setting SET value = ? WHERE key = 'sync.webdav'`, string(data))
		}
		return err
	})
}
