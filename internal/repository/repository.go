package repository

import (
	"context"
	"solar_project/internal/domain"
)

// Repository defines data access interface
type Repository interface {
	// Insert writes multiple records
	Insert(ctx context.Context, records []domain.InverterData) error
	
	// Query retrieves records based on filter
	Query(ctx context.Context, filter domain.QueryFilter) ([]domain.InverterData, error)
	
	// Count returns number of records matching filter
	Count(ctx context.Context, filter domain.QueryFilter) (int64, error)
	
	// Type returns database type
	Type() string
}