package db

import (
	"errors"

	"github.com/google/uuid"
)

func (l *Location) ValidatePost() error {
	if l.OwnerID == uuid.Nil {
		return errors.New("owner_id is required")
	}
	return nil
}

func (o *Organization) ValidatePost() error {
	if o.Name == "" {
		return errors.New("name is required")
	}
	if o.OwnerID == uuid.Nil {
		return errors.New("owner_id is required")
	}
	switch o.Type {
	case "", ORGANIZATION_TYPE_COMPANY, ORGANIZATION_TYPE_CHILD:
	case ORGANIZATION_TYPE_SYSTEM:
		return errors.New("type cannot be system")
	default:
		return errors.New("invalid type")
	}
	return nil
}

func (u *SamnaUser) ValidatePost() error {
	if u.Email == "" {
		return errors.New("email is required")
	}
	if u.Status != nil {
		switch UserStatus(*u.Status) {
		case USER_STATUS_ACTIVE, USER_STATUS_PENDING, USER_STATUS_SUSPENDED, USER_STATUS_INACTIVE:
		default:
			return errors.New("invalid status")
		}
	}
	return nil
}

func (c *Claim) ValidatePost() error {
	if c.Name == "" {
		return errors.New("name is required")
	}
	if c.OwnerID == uuid.Nil {
		return errors.New("owner_id is required")
	}
	if c.Access == 0 {
		return errors.New("access is required")
	}
	switch c.Type {
	case "", CLAIM_TYPE_USER, CLAIM_TYPE_SERVICE, CLAIM_TYPE_VIRTUAL:
	case CLAIM_TYPE_SYSTEM:
		return errors.New("type cannot be system")
	default:
		return errors.New("invalid type")
	}
	return nil
}

func (co *ClaimObject) ValidatePost() error {
	if co.ObjectID == nil || *co.ObjectID == uuid.Nil {
		return errors.New("object_id is required")
	}
	if co.ObjectType == nil || *co.ObjectType == "" {
		return errors.New("object_type is required")
	}
	return nil
}

func (uc *UserClaim) ValidatePost() error {
	if uc.UserID == uuid.Nil {
		return errors.New("user_id is required")
	}
	return nil
}

func (b *BodySecret) ValidatePost() error {
	if b.Key == nil || *b.Key == "" {
		return errors.New("key is required")
	}
	if b.Value == nil || *b.Value == "" {
		return errors.New("value is required")
	}
	if b.OwnerID == nil || *b.OwnerID == uuid.Nil {
		return errors.New("owner_id is required")
	}
	return nil
}

func (f *ObjectField) ValidatePost() error {
	if f.ObjectType == "" {
		return errors.New("object_type is required")
	}
	if f.Name == "" {
		return errors.New("name is required")
	}
	if f.OwnerID == uuid.Nil {
		return errors.New("owner_id is required")
	}
	return nil
}

func (f *UserField) ValidatePost() error {
	if f.UserID == uuid.Nil {
		return errors.New("user_id is required")
	}
	if f.OwnerID == uuid.Nil {
		return errors.New("owner_id is required")
	}
	return nil
}

func (r *UserRelation) ValidatePost() error {
	if r.ObjectType == "" {
		return errors.New("object_type is required")
	}
	if r.ObjectID == uuid.Nil {
		return errors.New("object_id is required")
	}
	return nil
}

func (a *Asset) ValidatePost() error {
	if a.Name == "" {
		return errors.New("name is required")
	}
	if a.MimeType == "" {
		return errors.New("mime_type is required")
	}
	if a.UserID == uuid.Nil {
		return errors.New("user_id is required")
	}
	return nil
}
