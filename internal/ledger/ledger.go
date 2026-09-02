// Package ledger: append-only write paths for raw documents, observations and
// content-addressed verdicts (PLAN.md §5, G2/G3/G4). No update paths exist.
package ledger

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"watchledger/internal/engine"
	"watchledger/internal/money"
)

// SaveRawDocument stores original source bytes before any extraction (G3).
// Returns the content hash and whether the document was new.
func SaveRawDocument(db *sql.DB, sourceID, url, contentType string, body []byte, fetchedAt time.Time) (hash string, isNew bool, err error) {
	sum := sha256.Sum256(body)
	hash = hex.EncodeToString(sum[:])
	res, err := db.Exec(`
		INSERT OR IGNORE INTO raw_documents (source_id, url, fetched_at, content_hash, content_type, body)
		VALUES (?, ?, ?, ?, ?, ?)`,
		sourceID, url, fetchedAt.Unix(), hash, contentType, body)
	if err != nil {
		return "", false, fmt.Errorf("save raw doc: %w", err)
	}
	n, _ := res.RowsAffected()
	return hash, n > 0, nil
}

// AppendObservation appends one price event to the ledger (G2).
// contentHash is the event identity: re-ingesting the same event is a no-op.
func AppendObservation(db *sql.DB, o Observation) (isNew bool, err error) {
	content := o.ContentHash()
	res, err := db.Exec(`
		INSERT OR IGNORE INTO observations
		(source_id, kind, brand, model, dial, material, scope, ref,
		 resolution_confidence, resolution_rung, title, url, raw_doc_id,
		 price, currency, price_usd, observed_at, content_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		o.SourceID, o.Kind, o.Brand, o.Model, o.Dial, o.Material, o.Scope, o.Ref,
		o.ResolutionConfidence, o.ResolutionRung, o.Title, o.URL, o.RawDocID,
		o.Price, o.Currency, o.PriceUSD, o.ObservedAt.Unix(), content)
	if err != nil {
		return false, fmt.Errorf("append observation: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Observation is the ledger row (write side). Read side assembles engine.Observation.
type Observation struct {
	SourceID              string
	Kind                  string // ask | sold | auction_realised | user_reported
	Brand, Model          string
	Dial, Material, Scope string
	Ref                   string
	ResolutionConfidence  float64
	ResolutionRung        int
	Title, URL            string
	RawDocID              sql.NullInt64
	Price, Currency       string // decimal string (G7)
	PriceUSD              string
	ObservedAt            time.Time
}

// ContentHash — the event's identity. Same source+listing+price+day = same event.
func (o Observation) ContentHash() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		o.SourceID, o.Kind, o.Ref, o.URL, o.Price, o.Currency,
		strconv.FormatInt(o.ObservedAt.Unix()/86400, 10), // day-precision dedup
	}, "\x1f")))
	return hex.EncodeToString(sum[:])
}

// SaveRuleset upserts a ruleset definition; same hash = no-op, different
// version = new row. Ruleset history is append-only too.
func SaveRuleset(db *sql.DB, r engine.Ruleset) error {
	def, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = db.Exec(`
		INSERT INTO rulesets (version, hash, definition, created_at)
		VALUES (?, ?, ?, strftime('%s','now'))
		ON CONFLICT(version) DO UPDATE SET hash=excluded.hash, definition=excluded.definition`,
		r.Version, r.Hash(), string(def))
	return err
}

// SaveVerdictContent stores a content-addressed verdict (G4).
// Same evidence + same rules = same primary key = no-op overwrite of identical content.
func SaveVerdictContent(db *sql.DB, v *engine.Verdict) error {
	if v.InputsHash == "" || v.RulesetHash == "" {
		return fmt.Errorf("verdict missing hashes: inputs=%q ruleset=%q", v.InputsHash, v.RulesetHash)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = db.Exec(`
		INSERT INTO verdict_content (inputs_hash, ruleset_hash, cell_key, result, created_at)
		VALUES (?, ?, ?, ?, strftime('%s','now'))
		ON CONFLICT(inputs_hash, ruleset_hash) DO UPDATE SET result=excluded.result`,
		v.InputsHash, v.RulesetHash, v.CellKey, string(b))
	return err
}

// LoadCellObservations assembles engine observations for one cell key.
func LoadCellObservations(db *sql.DB, cellKey string) ([]engine.Observation, error) {
	rows, err := db.Query(`
		SELECT brand, model, dial, material, scope, ref, kind, source_id,
		       title, url, price_usd, observed_at
		FROM observations
		WHERE LOWER(brand) || '|' || LOWER(model) || '|' || LOWER(dial) || '|' || LOWER(material) || '|' || LOWER(scope) = ?
		  AND price_usd IS NOT NULL`, cellKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []engine.Observation
	for rows.Next() {
		var o engine.Observation
		var priceUSD string
		var observedAt int64
		if err := rows.Scan(&o.Brand, &o.Model, &o.Dial, &o.Material, &o.Scope, &o.Ref,
			&o.Kind, &o.Source, &o.Title, &o.URL, &priceUSD, &observedAt); err != nil {
			return nil, err
		}
		d, err := money.FromString(priceUSD)
		if err != nil {
			return nil, fmt.Errorf("corrupt price %q: %w", priceUSD, err)
		}
		o.PriceUSD = d
		o.ObservedAt = time.Unix(observedAt, 0)
		out = append(out, o)
	}
	return out, rows.Err()
}

// LoadLatestVerdictContent returns the newest stored verdict for a cell under
// the current ruleset hash, if any.
func LoadLatestVerdictContent(db *sql.DB, cellKey, rulesetHash string) (string, error) {
	var result string
	err := db.QueryRow(`
		SELECT result FROM verdict_content
		WHERE cell_key = ? AND ruleset_hash = ?
		ORDER BY created_at DESC LIMIT 1`, cellKey, rulesetHash).Scan(&result)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return result, err
}
