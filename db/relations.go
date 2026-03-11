package db

import (
	"strings"

	"github.com/uptrace/bun"
)

type RelationFunc func() (string, func(q *bun.SelectQuery) *bun.SelectQuery)

var userRelations = map[string]RelationFunc{
	"assets":     RelationAssets,
	"identities": RelationIdentities,
	"clients":    RelationClients,
	"fields":     RelationFields,
	"claims":     RelationClaims,
}

var claimRelations = map[string]RelationFunc{
	"claim_objects": RelationClaimObjects,
	"claim_users":   RelationClaimUsers,
}

var organizationRelations = map[string]RelationFunc{
	"assets": RelationAssets,
}

var locationRelations = map[string]RelationFunc{
	"assets": RelationAssets,
}

func ParseWithRelations(withParam string, allowed map[string]RelationFunc) []RelationFunc {
	if withParam == "" {
		return nil
	}

	var result []RelationFunc
	for _, name := range strings.Split(withParam, ",") {
		name = strings.TrimSpace(name)
		if fn, ok := allowed[name]; ok {
			result = append(result, fn)
		}
	}
	return result
}

func ApplyRelations(q *bun.SelectQuery, relations []RelationFunc) *bun.SelectQuery {
	for _, fn := range relations {
		name, modifier := fn()
		q = q.Relation(name, modifier)
	}
	return q
}

func UserRelations() map[string]RelationFunc {
	return userRelations
}

func ClaimRelations() map[string]RelationFunc {
	return claimRelations
}

func OrganizationRelations() map[string]RelationFunc {
	return organizationRelations
}

func LocationRelations() map[string]RelationFunc {
	return locationRelations
}

func RelationAssets() (string, func(q *bun.SelectQuery) *bun.SelectQuery) {
	return "Assets", func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("sa_deleted_at IS NULL")
	}
}

func RelationIdentities() (string, func(q *bun.SelectQuery) *bun.SelectQuery) {
	return "Identities", func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("sa_deleted_at IS NULL")
	}
}

func RelationClients() (string, func(q *bun.SelectQuery) *bun.SelectQuery) {
	return "Clients", func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("sa_deleted_at IS NULL")
	}
}

func RelationFields() (string, func(q *bun.SelectQuery) *bun.SelectQuery) {
	return "Fields", func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("sa_deleted_at IS NULL")
	}
}

func RelationClaims() (string, func(q *bun.SelectQuery) *bun.SelectQuery) {
	return "Claims", func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("sa_deleted_at IS NULL")
	}
}

func RelationClaimObjects() (string, func(q *bun.SelectQuery) *bun.SelectQuery) {
	return "ClaimObjects", func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("sa_deleted_at IS NULL")
	}
}

func RelationClaimUsers() (string, func(q *bun.SelectQuery) *bun.SelectQuery) {
	return "ClaimUsers", func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("sa_deleted_at IS NULL")
	}
}
