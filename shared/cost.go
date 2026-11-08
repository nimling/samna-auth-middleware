package shared

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/uptrace/bun"
)

const (
	COST_SCHEMA_ID = "https://samna.io/schema/cost/1"

	COST_KIND_USAGE  = "usage"
	COST_KIND_FEE    = "fee"
	COST_KIND_PERIOD = "period"
	COST_KIND_CREDIT = "credit"

	COST_STATUS_ESTIMATED = "estimated"
	COST_STATUS_SETTLED   = "settled"
	COST_STATUS_INVOICED  = "invoiced"

	COST_GRANULARITY_HOUR  = "hour"
	COST_GRANULARITY_DAY   = "day"
	COST_GRANULARITY_MONTH = "month"

	costAmountTolerance = 0.005
)

var (
	ErrBadAppCost = errors.New("bad cost")

	costKinds               = []string{COST_KIND_USAGE, COST_KIND_FEE, COST_KIND_PERIOD, COST_KIND_CREDIT}
	costStatuses            = []string{COST_STATUS_ESTIMATED, COST_STATUS_SETTLED, COST_STATUS_INVOICED}
	costAccumulateMethods   = []string{"sum", "max", "last", "count"}
	costAccumulateProration = []string{"overlap", "start"}
)

var CostSchema = json.RawMessage(`{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://samna.io/schema/cost/1",
  "title": "Cost evidence",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "subject": {
      "type": "object",
      "required": ["type", "id"],
      "additionalProperties": false,
      "properties": {
        "type": {"type": "string", "pattern": "^[a-z_][a-z0-9_]*\\.[a-z_][a-z0-9_]*$"},
        "id": {"type": "string", "format": "uuid"},
        "name": {"type": "string"}
      }
    },
    "source": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "vendor": {"type": "string"},
        "ids": {"type": "array", "items": {"type": "string"}},
        "refs": {
          "type": "array",
          "items": {
            "type": "object",
            "required": ["table", "id"],
            "additionalProperties": false,
            "properties": {"table": {"type": "string"}, "id": {"type": "string"}}
          }
        },
        "run_id": {"type": "string"}
      }
    },
    "items": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["description", "quantity", "unit", "amount"],
        "additionalProperties": false,
        "properties": {
          "description": {"type": "string"},
          "quantity": {"type": "number"},
          "unit": {"type": "string", "minLength": 1},
          "unit_price": {"type": "number"},
          "amount": {"type": "number"},
          "ref": {"type": "string"},
          "at": {"type": "string", "format": "date-time"}
        }
      }
    },
    "pricing": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "list": {"type": "string"},
        "tier": {"type": "string"},
        "prorated": {
          "type": "object",
          "required": ["from", "to", "fraction"],
          "additionalProperties": false,
          "properties": {
            "from": {"type": "string", "format": "date-time"},
            "to": {"type": "string", "format": "date-time"},
            "fraction": {"type": "number", "minimum": 0, "maximum": 1}
          }
        }
      }
    },
    "notes": {"type": "string"}
  },
  "x-samna": {
    "title": "Cost",
    "columns": ["subject.name", "items", "pricing.tier", "notes"],
    "unit_label": "Unit",
    "amount_label": "Amount",
    "accumulate": {"method": "sum", "proration": "overlap", "group": ["subject.id"]}
  }
}`)

type AppCostBucket struct {
	AppID       uuid.UUID       `bun:"app_id" json:"app_id"`
	Module      string          `bun:"module" json:"module"`
	Kind        string          `bun:"kind" json:"kind"`
	SchemaID    *string         `bun:"schema_id" json:"schema_id"`
	GroupKey    json.RawMessage `bun:"group_key,type:jsonb" json:"group_key"`
	PeriodStart time.Time       `bun:"period_start" json:"period_start"`
	Unit        string          `bun:"unit" json:"unit"`
	Currency    string          `bun:"currency" json:"currency"`
	Quantity    float64         `bun:"quantity" json:"quantity"`
	Amount      float64         `bun:"amount" json:"amount"`
	Hours       float64         `bun:"hours" json:"hours"`
	Rows        int64           `bun:"rows" json:"rows"`
}

type AppCostQuery struct {
	Module      string
	Status      string
	SubjectType string
	SubjectID   uuid.UUID
	From        time.Time
	To          time.Time
	Granularity string
}

func GetAppCost(ctx context.Context, db *DB, userID, appID uuid.UUID, query AppCostQuery) ([]AppCostBucket, error) {
	var module, status, subjectType *string
	if query.Module != "" {
		module = &query.Module
	}
	if query.Status != "" {
		status = &query.Status
	}
	if query.SubjectType != "" {
		subjectType = &query.SubjectType
	}
	var subjectID *uuid.UUID
	if query.SubjectID != uuid.Nil {
		subjectID = &query.SubjectID
	}
	var from, to *time.Time
	if !query.From.IsZero() {
		from = &query.From
	}
	if !query.To.IsZero() {
		to = &query.To
	}
	granularity := query.Granularity
	if granularity == "" {
		granularity = COST_GRANULARITY_HOUR
	}
	buckets := make([]AppCostBucket, 0)
	q := db.NewRaw("SELECT * FROM claimius.get_app_cost(?, ?, ?, ?, ?, ?, ?, ?)", userID, module, from, to, granularity, status, subjectType, subjectID)
	if db.Prophet {
		q = db.NewRaw("SELECT * FROM claimius.get_app_cost(?, ?, ?, ?, ?, ?, ?, ?, ?)", userID, appID, module, from, to, granularity, status, subjectType, subjectID)
	}
	if err := q.Scan(ctx, &buckets); err != nil {
		return nil, err
	}
	return buckets, nil
}

func costError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrBadAppCost, fmt.Sprintf(format, args...))
}

func costAmountMatches(amount, quantity, unitPrice float64) bool {
	return math.Abs(amount-quantity*unitPrice) <= costAmountTolerance
}

func (c *AppCostInput) form() string {
	switch {
	case c.At != nil:
		return "event"
	case len(c.Items) > 0:
		return "items"
	}
	return "row"
}

func (c *AppCostInput) ValidatePost() error {
	if unknown := c.GetUnknown(); len(unknown) > 0 {
		return costError("%s not allowed", unknown[0])
	}
	if strings.TrimSpace(c.Module) == "" {
		return costError("module required")
	}
	if strings.TrimSpace(c.Ref) == "" {
		return costError("ref required")
	}
	if !appCostCurrency.MatchString(c.Currency) {
		return costError("currency must be an ISO 4217 code")
	}
	if c.Status != "" && !contains(costStatuses, c.Status) {
		return costError("status must be one of estimated, settled, invoiced")
	}
	if c.Kind != "" && !contains(costKinds, c.Kind) {
		return costError("kind must be one of usage, fee, period, credit")
	}
	var detail map[string]any
	if len(c.Detail) > 0 {
		if err := json.Unmarshal(c.Detail, &detail); err != nil || detail == nil {
			return costError("detail must be a JSON object")
		}
	}
	switch c.form() {
	case "event":
		for _, key := range []struct {
			name string
			set  bool
		}{{"period_start", c.PeriodStart != nil}, {"period_end", c.PeriodEnd != nil}, {"quantity", c.Quantity != nil}, {"unit", c.Unit != ""}, {"unit_price", c.UnitPrice != nil}, {"items", len(c.Items) > 0}} {
			if key.set {
				return costError("%s not allowed with at", key.name)
			}
		}
		if c.Kind != "" && c.Kind != COST_KIND_FEE {
			return costError("kind must be fee with at")
		}
		if c.Amount == nil {
			return costError("amount required with at")
		}
	case "items":
		for _, key := range []struct {
			name string
			set  bool
		}{{"quantity", c.Quantity != nil}, {"unit", c.Unit != ""}, {"unit_price", c.UnitPrice != nil}, {"amount", c.Amount != nil}} {
			if key.set {
				return costError("%s not allowed with items", key.name)
			}
		}
		if _, has := detail["items"]; has {
			return costError("detail.items not allowed with items")
		}
		for n, item := range c.Items {
			if item.Description == "" {
				return costError("items[%d].description required", n)
			}
			if item.Unit == "" {
				return costError("items[%d].unit required", n)
			}
			if item.UnitPrice == nil && item.Amount == nil {
				return costError("items[%d] needs unit_price or amount", n)
			}
			if item.UnitPrice != nil && item.Amount != nil && !costAmountMatches(*item.Amount, item.Quantity, *item.UnitPrice) {
				return costError("items[%d].amount must equal quantity times unit_price", n)
			}
		}
	default:
		if c.Kind == "" {
			return costError("kind required")
		}
		if c.Quantity == nil {
			return costError("quantity required")
		}
		if c.Unit == "" {
			return costError("unit required")
		}
		if c.Amount == nil && c.UnitPrice == nil {
			return costError("amount or unit_price required")
		}
		if c.Amount != nil && c.UnitPrice != nil && !costAmountMatches(*c.Amount, *c.Quantity, *c.UnitPrice) {
			return costError("amount must equal quantity times unit_price")
		}
	}
	if c.At == nil {
		if c.PeriodStart == nil {
			return costError("period_start required")
		}
		if c.PeriodEnd == nil {
			return costError("period_end required")
		}
		if c.PeriodEnd.Before(*c.PeriodStart) {
			return costError("period_end must not be before period_start")
		}
		if c.Kind == COST_KIND_FEE && !c.PeriodEnd.Equal(*c.PeriodStart) {
			return costError("a fee has period_end equal to period_start")
		}
	}
	return nil
}

func (c *AppCostInput) Normalize() (*AppCost, error) {
	if err := c.ValidatePost(); err != nil {
		return nil, err
	}
	cost := &AppCost{
		Module:       c.Module,
		Kind:         c.Kind,
		Currency:     c.Currency,
		Ref:          c.Ref,
		Status:       c.Status,
		SupersedesID: c.SupersedesID,
		Schema:       c.Schema,
		Detail:       c.Detail,
		CreatedBy:    c.CreatedBy,
	}
	if cost.Status == "" {
		cost.Status = COST_STATUS_SETTLED
	}
	switch c.form() {
	case "event":
		cost.Kind = COST_KIND_FEE
		cost.PeriodStart = c.At.UTC()
		cost.PeriodEnd = c.At.UTC()
		cost.Quantity = 1
		cost.Unit = "event"
		cost.Amount = *c.Amount
	case "items":
		if cost.Kind == "" {
			cost.Kind = COST_KIND_USAGE
		}
		cost.PeriodStart = c.PeriodStart.UTC()
		cost.PeriodEnd = c.PeriodEnd.UTC()
		items := make([]AppCostItem, len(c.Items))
		sameUnit := true
		for n, item := range c.Items {
			if item.Amount == nil {
				amount := item.Quantity * *item.UnitPrice
				item.Amount = &amount
			}
			items[n] = item
			cost.Amount += *item.Amount
			cost.Quantity += item.Quantity
			sameUnit = sameUnit && item.Unit == c.Items[0].Unit
		}
		cost.Unit = c.Items[0].Unit
		if !sameUnit {
			cost.Quantity = float64(len(items))
			cost.Unit = "item"
		}
		detail := map[string]any{}
		if len(c.Detail) > 0 {
			if err := json.Unmarshal(c.Detail, &detail); err != nil {
				return nil, costError("detail must be a JSON object")
			}
		}
		detail["items"] = items
		raw, err := json.Marshal(detail)
		if err != nil {
			return nil, err
		}
		cost.Detail = raw
		if len(bytes.TrimSpace(cost.Schema)) == 0 {
			cost.Schema = CostSchema
		}
	default:
		cost.PeriodStart = c.PeriodStart.UTC()
		cost.PeriodEnd = c.PeriodEnd.UTC()
		cost.Quantity = *c.Quantity
		cost.Unit = c.Unit
		cost.UnitPrice = c.UnitPrice
		if c.Amount != nil {
			cost.Amount = *c.Amount
		} else {
			cost.Amount = *c.Quantity * *c.UnitPrice
		}
	}
	if cost.Kind != COST_KIND_CREDIT && cost.Amount < 0 {
		return nil, costError("amount must not be negative for kind %s", cost.Kind)
	}
	if err := cost.Normalize(); err != nil {
		return nil, err
	}
	return cost, nil
}

func (c *AppCost) Normalize() error {
	detail := map[string]any{}
	if len(bytes.TrimSpace(c.Detail)) > 0 {
		if err := json.Unmarshal(c.Detail, &detail); err != nil || detail == nil {
			return costError("detail must be a JSON object")
		}
	}
	doc := map[string]any{}
	if len(bytes.TrimSpace(c.Schema)) > 0 {
		if err := json.Unmarshal(c.Schema, &doc); err != nil || doc == nil {
			return costError("schema must be a JSON object")
		}
	}
	if len(doc) == 0 {
		raw, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		c.Schema = json.RawMessage(`{}`)
		c.Detail = raw
		return nil
	}
	if err := checkCostPresentation(doc); err != nil {
		return err
	}
	if id, _ := doc["$id"].(string); id == COST_SCHEMA_ID {
		if err := normalizeCostItems(detail, c.Amount); err != nil {
			return err
		}
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	if err := compiler.AddResource("cost.json", doc); err != nil {
		return costError("schema: %v", err)
	}
	schema, err := compiler.Compile("cost.json")
	if err != nil {
		return costError("schema: %v", err)
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	instance, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return costError("detail must be a JSON object")
	}
	if err := schema.Validate(instance); err != nil {
		var failed *jsonschema.ValidationError
		if !errors.As(err, &failed) {
			return costError("detail: %v", err)
		}
		leaf := failed
		for len(leaf.Causes) > 0 {
			leaf = leaf.Causes[0]
		}
		return costError("detail %s", leaf.Error())
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, c.Schema); err != nil {
		return costError("schema must be a JSON object")
	}
	c.Schema = compact.Bytes()
	c.Detail = raw
	return nil
}

func checkCostPresentation(doc map[string]any) error {
	raw, present := doc["x-samna"]
	if !present {
		return nil
	}
	hints, ok := raw.(map[string]any)
	if !ok {
		return costError("schema x-samna must be an object")
	}
	raw, present = hints["accumulate"]
	if !present {
		return nil
	}
	accumulate, ok := raw.(map[string]any)
	if !ok {
		return costError("schema x-samna.accumulate must be an object")
	}
	if method, present := accumulate["method"]; present {
		if s, ok := method.(string); !ok || !contains(costAccumulateMethods, s) {
			return costError("schema x-samna.accumulate.method must be one of sum, max, last, count")
		}
	}
	if proration, present := accumulate["proration"]; present {
		if s, ok := proration.(string); !ok || !contains(costAccumulateProration, s) {
			return costError("schema x-samna.accumulate.proration must be one of overlap, start")
		}
	}
	if group, present := accumulate["group"]; present {
		paths, ok := group.([]any)
		if !ok {
			return costError("schema x-samna.accumulate.group must be an array of paths into detail")
		}
		for _, path := range paths {
			if s, ok := path.(string); !ok || s == "" {
				return costError("schema x-samna.accumulate.group must be an array of paths into detail")
			}
		}
	}
	return nil
}

func normalizeCostItems(detail map[string]any, amount float64) error {
	raw, present := detail["items"]
	if !present {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	sum := 0.0
	for _, element := range list {
		item, ok := element.(map[string]any)
		if !ok {
			continue
		}
		quantity, _ := item["quantity"].(float64)
		if _, has := item["amount"]; !has {
			if unitPrice, ok := item["unit_price"].(float64); ok {
				item["amount"] = quantity * unitPrice
			}
		}
		if unitPrice, ok := item["unit_price"].(float64); ok {
			if itemAmount, ok := item["amount"].(float64); ok && !costAmountMatches(itemAmount, quantity, unitPrice) {
				return costError("detail.items amount must equal quantity times unit_price")
			}
		}
		itemAmount, _ := item["amount"].(float64)
		sum += itemAmount
	}
	if math.Abs(sum-amount) > costAmountTolerance {
		return costError("detail.items amounts sum to %v, amount is %v", sum, amount)
	}
	return nil
}

func (d *DB) putAppCost(ctx context.Context, conn bun.IConn, ac *AccessClaim, input *AppCostInput) (*AppCost, bool, error) {
	if ac == nil || ac.UserClaimID == nil {
		return nil, false, ErrNoAccess
	}
	cost, err := input.Normalize()
	if err != nil {
		return nil, false, err
	}
	cost.OwnerID = ac.OwnerID
	cost.CreatedBy = ac.UserClaimID
	cost.DeletedAt = nil
	if cost.SupersedesID != nil {
		var ref string
		if err := d.DB.NewSelect().Conn(conn).
			Model((*AppCost)(nil)).
			Column("ref").
			Where("id = ?", *cost.SupersedesID).
			Where("sa_deleted_at IS NULL").
			Scan(ctx, &ref); err != nil {
			return nil, false, fmt.Errorf("%w: supersedes_id names no live row", ErrNotFound)
		}
		if ref == cost.Ref {
			return nil, false, costError("supersedes_id must name another row")
		}
	}
	if _, err := d.DB.NewInsert().Conn(conn).
		Model(cost).
		On("CONFLICT (module, ref) WHERE sa_deleted_at IS NULL DO UPDATE").
		Set("kind = EXCLUDED.kind").
		Set("period_start = EXCLUDED.period_start").
		Set("period_end = EXCLUDED.period_end").
		Set("quantity = EXCLUDED.quantity").
		Set("unit = EXCLUDED.unit").
		Set("unit_price = EXCLUDED.unit_price").
		Set("amount = EXCLUDED.amount").
		Set("currency = EXCLUDED.currency").
		Set("supersedes_id = EXCLUDED.supersedes_id").
		Set("status = EXCLUDED.status").
		Set("schema = EXCLUDED.schema").
		Set("detail = EXCLUDED.detail").
		Set("sa_created_by = EXCLUDED.sa_created_by").
		Returning("*").
		Exec(ctx); err != nil {
		return nil, false, err
	}
	return cost, cost.CreatedAt.Equal(cost.UpdatedAt), nil
}

func (d *DB) deleteAppCost(ctx context.Context, conn bun.IConn, ac *AccessClaim, costID uuid.UUID) (*AppCost, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	var cost AppCost
	_, err := d.DB.NewUpdate().Conn(conn).
		Model(&cost).
		Where("id = ?", costID).
		Where("sa_deleted_at IS NULL").
		Set("sa_deleted_at = NOW()").
		Returning("*").
		Exec(ctx, &cost)
	if err != nil {
		return nil, err
	}
	return &cost, nil
}
