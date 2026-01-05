package hook

import (
	"context"
	"encoding/json"
	"fmt"
)

type ActionHandler[T any] struct {
	OnCreate func(context.Context, T) error
	OnUpdate func(context.Context, T) error
	OnDelete func(context.Context, T) error
}

type Handlers struct {
	User         ActionHandler[User]
	Organization ActionHandler[Organization]
	Location     ActionHandler[Location]
	Claim        ActionHandler[Claim]
	UserClaim    ActionHandler[UserClaim]
	ObjectClaim  ActionHandler[ClaimObject]
}

func (p *Payload) UnmarshalAndHandle(ctx context.Context, h Handlers) error {
	switch p.EntityType {
	case EntityTypeUser:
		var u User
		if err := json.Unmarshal(p.Data, &u); err != nil {
			return fmt.Errorf("unmarshal user: %w", err)
		}
		return callAction(ctx, p.EventType, u, h.User)

	case EntityTypeOrganization:
		var o Organization
		if err := json.Unmarshal(p.Data, &o); err != nil {
			return fmt.Errorf("unmarshal organization: %w", err)
		}
		return callAction(ctx, p.EventType, o, h.Organization)

	case EntityTypeLocation:
		var l Location
		if err := json.Unmarshal(p.Data, &l); err != nil {
			return fmt.Errorf("unmarshal location: %w", err)
		}
		return callAction(ctx, p.EventType, l, h.Location)

	case EntityTypeClaim:
		var c Claim
		if err := json.Unmarshal(p.Data, &c); err != nil {
			return fmt.Errorf("unmarshal claim: %w", err)
		}
		return callAction(ctx, p.EventType, c, h.Claim)

	case EntityTypeObjectClaim:
		var oc ClaimObject
		if err := json.Unmarshal(p.Data, &oc); err != nil {
			return fmt.Errorf("unmarshal object claim: %w", err)
		}
		return callAction(ctx, p.EventType, oc, h.ObjectClaim)

	case EntityTypeUserClaim:
		var uc UserClaim
		if err := json.Unmarshal(p.Data, &uc); err != nil {
			return fmt.Errorf("unmarshal user claim: %w", err)
		}
		return callAction(ctx, p.EventType, uc, h.UserClaim)

	default:
		return fmt.Errorf("unknown entity type: %s", p.EntityType)
	}
}

func callAction[T any](ctx context.Context, et EventType, val T, h ActionHandler[T]) error {
	switch et {
	case EventTypeCreate:
		if h.OnCreate != nil {
			return h.OnCreate(ctx, val)
		}
	case EventTypeUpdate:
		if h.OnUpdate != nil {
			return h.OnUpdate(ctx, val)
		}
	case EventTypeDelete:
		if h.OnDelete != nil {
			return h.OnDelete(ctx, val)
		}
	}
	return nil
}
