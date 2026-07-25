package services

// credential_service.go — the DB-backed credential store behind
// GET/POST/PUT/DELETE /api/credentials (docs/credentials-contract.md §4).
//
// Secret values are WRITE-ONLY: every read model is a CredentialSummary that
// exposes WHICH keys are set (setKeys), never the values. The runtime reads the
// values straight from Postgres (reasoner for http/builtin nodes, pieces-worker
// for pieces nodes) — the API never hands them out.
//
// `type` is validated against the known credential-type ids. The canonical
// source is the generated credential-types file (written by
// packages/nodes/build-web.mjs per contract §3); when it is present its field
// definitions also drive required-field validation on create. On a stripped
// deployment without that file we degrade to deriving the type-id set from the
// loaded catalog + pieces index (same ids, no field metadata — required-field
// validation is then skipped, mirroring the catalog's fail-open discipline).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/agently/api/internal/domain"
	"github.com/agently/api/internal/platform"
)

// CredentialSummary is the §4 read model: identity + which keys are set.
type CredentialSummary struct {
	ID        domain.CredentialId `json:"id"`
	Name      string              `json:"name"`
	Type      string              `json:"type"`
	SetKeys   []string            `json:"setKeys"`
	CreatedAt domain.Timestamp    `json:"createdAt"`
	UpdatedAt domain.Timestamp    `json:"updatedAt"`
}

// CredentialTypeField mirrors one §1-shaped field of a credential type.
type CredentialTypeField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Control  string `json:"control"`
	Required bool   `json:"required"`
	Help     string `json:"help,omitempty"`
}

// CredentialTypeDef mirrors one entry of credential-types.generated.json (§3).
type CredentialTypeDef struct {
	ID       string                `json:"id"`
	Label    string                `json:"label"`
	Source   string                `json:"source"`
	AuthType string                `json:"authType,omitempty"`
	Fields   []CredentialTypeField `json:"fields"`
}

// CredentialService owns credential CRUD for the current workspace.
type CredentialService struct {
	deps Deps
}

func NewCredentialService(deps Deps) *CredentialService {
	return &CredentialService{deps: deps}
}

func (s *CredentialService) List() []CredentialSummary {
	ws := s.deps.Repos.Workspace.Get()
	rows := s.deps.Repos.Credentials.ListByWorkspace(ws.ID)
	out := make([]CredentialSummary, 0, len(rows))
	for _, c := range rows {
		out = append(out, summarize(c))
	}
	return out
}

func (s *CredentialService) Create(name, typ string, values map[string]any) (CredentialSummary, error) {
	if err := validateCredentialType(typ, values, true); err != nil {
		return CredentialSummary{}, err
	}
	now := domain.Timestamp(s.deps.Clock.ISO())
	c := domain.Credential{
		ID:          domain.NewCredentialId(),
		WorkspaceID: s.deps.Repos.Workspace.Get().ID,
		Type:        typ,
		Name:        name,
		Data:        cleanValues(values),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	return summarize(s.deps.Repos.Credentials.Insert(c)), nil
}

// Update renames and/or merges secret values PER-KEY: keys absent from values
// are preserved; an empty-string value clears that key (§4).
func (s *CredentialService) Update(id domain.CredentialId, name *string, values map[string]any) (CredentialSummary, error) {
	existing, ok := s.deps.Repos.Credentials.GetByID(id)
	if !ok || existing.WorkspaceID != s.deps.Repos.Workspace.Get().ID {
		return CredentialSummary{}, domain.NotFound("Credential")
	}
	now := domain.Timestamp(s.deps.Clock.ISO())
	patch := platform.CredentialPatch{Name: name, UpdatedAt: &now}
	if values != nil {
		merged := map[string]any{}
		for k, v := range existing.Data {
			merged[k] = v
		}
		for k, v := range values {
			if sv, isStr := v.(string); isStr && sv == "" {
				delete(merged, k) // explicit clear
				continue
			}
			merged[k] = v
		}
		patch.Data = &merged
	}
	updated, err := s.deps.Repos.Credentials.Update(id, patch)
	if err != nil {
		return CredentialSummary{}, err
	}
	return summarize(updated), nil
}

func (s *CredentialService) Delete(id domain.CredentialId) error {
	existing, ok := s.deps.Repos.Credentials.GetByID(id)
	if !ok || existing.WorkspaceID != s.deps.Repos.Workspace.Get().ID {
		return domain.NotFound("Credential")
	}
	if !s.deps.Repos.Credentials.Delete(id) {
		return domain.NotFound("Credential")
	}
	return nil
}

/* ------------------------------ internals ------------------------------ */

func summarize(c domain.Credential) CredentialSummary {
	keys := make([]string, 0, len(c.Data))
	for k, v := range c.Data {
		if sv, isStr := v.(string); isStr && sv == "" {
			continue
		}
		if v == nil {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return CredentialSummary{
		ID: c.ID, Name: c.Name, Type: c.Type, SetKeys: keys,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

// cleanValues drops empty/nil entries so setKeys always reflects real secrets.
func cleanValues(values map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range values {
		if v == nil {
			continue
		}
		if sv, isStr := v.(string); isStr && sv == "" {
			continue
		}
		out[k] = v
	}
	return out
}

// validateCredentialType checks typ against the known type ids and, when field
// metadata is available and requireFields is true, that every required field
// has a value.
func validateCredentialType(typ string, values map[string]any, requireFields bool) error {
	if strings.TrimSpace(typ) == "" {
		return domain.BadRequest("type is required")
	}
	types, haveFields := credentialTypes()
	def, known := types[typ]
	if !known {
		return domain.BadRequest("unknown credential type '" + typ + "'")
	}
	if !requireFields || !haveFields {
		return nil
	}
	var missing []string
	for _, f := range def.Fields {
		if !f.Required {
			continue
		}
		v, present := values[f.Key]
		if !present || v == nil {
			missing = append(missing, f.Key)
			continue
		}
		if sv, isStr := v.(string); isStr && sv == "" {
			missing = append(missing, f.Key)
		}
	}
	if len(missing) > 0 {
		return domain.BadRequest("missing required credential field(s): " + strings.Join(missing, ", "))
	}
	return nil
}

var (
	credTypesOnce sync.Once
	credTypesVal  map[string]CredentialTypeDef
	credTypesRich bool // true when loaded from the generated file (fields known)
)

// credentialTypes returns the known credential types, loaded once per process.
func credentialTypes() (map[string]CredentialTypeDef, bool) {
	credTypesOnce.Do(func() {
		credTypesVal, credTypesRich = loadCredentialTypes()
	})
	return credTypesVal, credTypesRich
}

func loadCredentialTypes() (map[string]CredentialTypeDef, bool) {
	if path := credentialTypesPath(); path != "" {
		raw, err := os.ReadFile(path)
		if err == nil {
			var m map[string]CredentialTypeDef
			if err := json.Unmarshal(raw, &m); err == nil && len(m) > 0 {
				return m, true
			}
		}
	}
	// Fallback: derive the id set from the catalog + pieces index (no fields).
	m := map[string]CredentialTypeDef{}
	cat := LoadCatalog()
	for id, n := range cat.ByID {
		if len(n.Credentials) == 0 {
			continue
		}
		var typeID string
		if strings.HasPrefix(id, "pieces.") {
			slug := pieceSlugOf(id)
			if slug == "" {
				continue
			}
			typeID = "pieces." + slug
		} else {
			typeID = strings.SplitN(id, ".", 2)[0]
		}
		if _, ok := m[typeID]; !ok {
			m[typeID] = CredentialTypeDef{ID: typeID, Label: typeID}
		}
	}
	return m, false
}

// credentialTypesPath resolves the generated types file: CREDENTIAL_TYPES_PATH
// wins; otherwise walk up from the working directory (same discipline as
// catalogDir / piecesIndexPath).
func credentialTypesPath() string {
	if v := os.Getenv("CREDENTIAL_TYPES_PATH"); v != "" {
		return v
	}
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 8; i++ {
		cand := filepath.Join(dir, "apps", "web", "components", "builder", "credential-types.generated.json")
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
