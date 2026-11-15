package tenant

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// APIBusiness is GET /v1/business's answer (issue #37, ADR 0047 §8.4, OA-2): the business an API key
// or an OAuth grant speaks for -- its id, its name and its company code (tenants.api_code; nil until
// the operator opens the business's API, which an OAuth connection does not need -- §11.1).
type APIBusiness struct {
	ID      uuid.UUID
	Name    string
	APICode *string
}

// BusinessCard reads it. Dependencies are fields (§7).
type BusinessCard struct{ data Database }

// NewBusinessCard wires the reader.
func NewBusinessCard(data Database) (*BusinessCard, error) {
	if data == nil {
		return nil, errors.New("tenant: nil database")
	}
	return &BusinessCard{data: data}, nil
}

// APIBusiness reads the business in its own context (WithTenant + the explicit id -- the ONE tenant
// the credential resolved to, never one a request names), under APIReadTimeout.
func (b *BusinessCard) APIBusiness(ctx context.Context, tenantID uuid.UUID) (APIBusiness, error) {
	if tenantID == uuid.Nil {
		return APIBusiness{}, errors.New("tenant: tenant id is required")
	}
	var out APIBusiness
	err := b.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if err := boundAPIRead(ctx, tx); err != nil {
			return err
		}
		row, err := store.New(tx).GetAPIBusiness(ctx, tenantID)
		if err != nil {
			return err
		}
		out = APIBusiness{ID: row.ID, Name: row.Name, APICode: row.ApiCode}
		return nil
	})
	if err != nil {
		return APIBusiness{}, fmt.Errorf("tenant: read the business: %w", err)
	}
	return out, nil
}
