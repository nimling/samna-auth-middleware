package db

import (
	"github.com/uptrace/bun"
)

func RelationClaimObjects() (string, func(q *bun.SelectQuery) *bun.SelectQuery) {
	return "ClaimObjects", func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("claim_object.sa_deleted_at IS NULL")
	}
}

func RelationClaimUsers() (string, func(q *bun.SelectQuery) *bun.SelectQuery) {
	return "ClaimUsers", func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("user_claim.sa_deleted_at IS NULL")
	}
}

func RelationParent() (string, func(q *bun.SelectQuery) *bun.SelectQuery) {
	return "Parent", func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("parent.sa_deleted_at IS NULL")
	}
}

func RelationOwner() (string, func(q *bun.SelectQuery) *bun.SelectQuery) {
	return "Owner", func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("owner.sa_deleted_at IS NULL")
	}
}

func RelationUser() (string, func(q *bun.SelectQuery) *bun.SelectQuery) {
	return "User", func(q *bun.SelectQuery) *bun.SelectQuery {
		return q.Where("user.sa_deleted_at IS NULL")
	}
}
