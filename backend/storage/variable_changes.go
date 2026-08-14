package storage

import (
	"database/sql"
	"encoding/json"
	"errors"

	"apirequest/backend/model"
	"apirequest/backend/secrets"
)

// VariableMutation is a script-produced key-level change for one scope.
type VariableMutation struct {
	Set   map[string]string
	Unset map[string]bool
}

func (m VariableMutation) empty() bool { return len(m.Set) == 0 && len(m.Unset) == 0 }

// WorkspaceVariableMutations groups one Request Execution's buffered changes.
type WorkspaceVariableMutations struct {
	EnvironmentId string
	CollectionId  string
	Environment   VariableMutation
	Collection    VariableMutation
	Globals       VariableMutation
}

// ApplyWorkspaceVariableMutations merges against the latest scope values and
// commits every changed scope with one SQL transaction and one Vault batch.
func (s *Store) ApplyWorkspaceVariableMutations(workspaceId string, changes WorkspaceVariableMutations) error {
	if changes.Environment.empty() && changes.Collection.empty() && changes.Globals.empty() {
		return nil
	}
	return s.withSecretWrite(func(writer secrets.SecretWriter) error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		now := nowMs()

		if !changes.Environment.empty() {
			if changes.EnvironmentId == "" {
				return errors.New("environment id is required for variable changes")
			}
			if err := s.applyEnvironmentVariableMutationTx(tx, writer, workspaceId, changes.EnvironmentId, changes.Environment, now); err != nil {
				return err
			}
		}
		if !changes.Collection.empty() {
			if changes.CollectionId == "" {
				return errors.New("collection id is required for variable changes")
			}
			if err := s.applyCollectionVariableMutationTx(tx, writer, workspaceId, changes.CollectionId, changes.Collection, now); err != nil {
				return err
			}
		}
		if !changes.Globals.empty() {
			if err := s.applyGlobalVariableMutationTx(tx, writer, workspaceId, changes.Globals, now); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
}

func (s *Store) applyEnvironmentVariableMutationTx(tx *sql.Tx, writer secrets.SecretWriter, workspaceId, environmentId string, mutation VariableMutation, updatedAt int64) error {
	var raw string
	if err := tx.QueryRow("SELECT variables FROM environment WHERE id = ? AND workspace_id = ?", environmentId, workspaceId).Scan(&raw); err != nil {
		return err
	}
	stored, current, err := s.decodeStoredVariables(raw)
	if err != nil {
		return err
	}
	protected, err := protectVariables(writer, applyVariableMutation(current, mutation), "environment/"+environmentId)
	if err != nil {
		return err
	}
	if err := deleteRemovedSecretReferences(writer, secrets.VariableReferences(stored), secrets.VariableReferences(protected)); err != nil {
		return err
	}
	data, err := json.Marshal(protected)
	if err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE environment SET variables = ?, updated_at = ? WHERE id = ? AND workspace_id = ?", string(data), updatedAt, environmentId, workspaceId)
	return err
}

func (s *Store) applyCollectionVariableMutationTx(tx *sql.Tx, writer secrets.SecretWriter, workspaceId, collectionId string, mutation VariableMutation, updatedAt int64) error {
	var raw sql.NullString
	if err := tx.QueryRow("SELECT variables FROM node WHERE id = ? AND workspace_id = ? AND deleted_at IS NULL", collectionId, workspaceId).Scan(&raw); err != nil {
		return err
	}
	stored, current, err := s.decodeStoredVariables(raw.String)
	if err != nil {
		return err
	}
	protected, err := protectVariables(writer, applyVariableMutation(current, mutation), "node/"+collectionId)
	if err != nil {
		return err
	}
	if err := deleteRemovedSecretReferences(writer, secrets.VariableReferences(stored), secrets.VariableReferences(protected)); err != nil {
		return err
	}
	data, err := json.Marshal(protected)
	if err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE node SET variables = NULLIF(?, ''), updated_at = ? WHERE id = ? AND workspace_id = ?", string(data), updatedAt, collectionId, workspaceId)
	return err
}

func (s *Store) applyGlobalVariableMutationTx(tx *sql.Tx, writer secrets.SecretWriter, workspaceId string, mutation VariableMutation, updatedAt int64) error {
	var raw string
	err := tx.QueryRow("SELECT variables FROM global_var WHERE workspace_id = ?", workspaceId).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		raw = "[]"
	} else if err != nil {
		return err
	}
	stored, current, err := s.decodeStoredVariables(raw)
	if err != nil {
		return err
	}
	protected, err := protectVariables(writer, applyVariableMutation(current, mutation), "workspace/"+workspaceId+"/globals")
	if err != nil {
		return err
	}
	if err := deleteRemovedSecretReferences(writer, secrets.VariableReferences(stored), secrets.VariableReferences(protected)); err != nil {
		return err
	}
	data, err := json.Marshal(protected)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO global_var (workspace_id, variables, updated_at) VALUES (?,?,?)
		ON CONFLICT(workspace_id) DO UPDATE SET variables = excluded.variables, updated_at = excluded.updated_at`, workspaceId, string(data), updatedAt)
	return err
}

func (s *Store) decodeStoredVariables(raw string) ([]model.Variable, []model.Variable, error) {
	stored := []model.Variable{}
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &stored); err != nil {
			return nil, nil, err
		}
	}
	current, err := secrets.ResolveVariables(s.vault, stored)
	return stored, current, err
}

func applyVariableMutation(variables []model.Variable, mutation VariableMutation) []model.Variable {
	out := make([]model.Variable, 0, len(variables)+len(mutation.Set))
	seen := map[string]bool{}
	for _, variable := range variables {
		if mutation.Unset[variable.Key] {
			continue
		}
		if value, ok := mutation.Set[variable.Key]; ok {
			variable.Value = value
			variable.Enabled = true
			seen[variable.Key] = true
		}
		out = append(out, variable)
	}
	for key, value := range mutation.Set {
		if !seen[key] {
			out = append(out, model.Variable{Key: key, Value: value, Type: "default", Enabled: true})
		}
	}
	return out
}
