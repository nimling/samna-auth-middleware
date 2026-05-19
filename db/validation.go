package db

import (
	"errors"
	"net/mail"

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
	if _, err := mail.ParseAddress(u.Email); err != nil {
		return errors.New("email must be a valid address")
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

func (t *SamnaUserToken) ValidatePost() error {
	if t.Token == "" {
		return errors.New("token is required")
	}
	if t.User != nil && t.User.ExternalID != nil && *t.User.ExternalID != "" {
		return errors.New("user.external_id is not accepted; it is derived from the token claim")
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

func (l *Location) ValidatePut() error {
	if l.Name == nil && l.Description == nil && l.Type == "" && l.ParentID == nil && l.Longitude == nil && l.Latitude == nil && l.Level == 0 && l.CodeType == nil && l.Code == nil && l.ExternalID == nil {
		return errors.New("no fields to update")
	}
	return nil
}

func (l *Location) ValidatePatch() error {
	if l.IsEmpty() {
		return errors.New("no fields to update")
	}
	if err := RejectImmutableKeys(l); err != nil {
		return err
	}
	if !hasAnyJSONKey(l) {
		return errors.New("no fields to update")
	}
	if l.HasKey("name") && (l.Name == nil || *l.Name == "") {
		return errors.New("name is required")
	}
	return nil
}

func (o *Organization) ValidatePut() error {
	if err := RejectImmutableKeys(o); err != nil {
		return err
	}
	if o.Name == "" {
		return errors.New("name is required")
	}
	switch o.Type {
	case ORGANIZATION_TYPE_COMPANY, ORGANIZATION_TYPE_CHILD:
	case "":
		return errors.New("type is required")
	case ORGANIZATION_TYPE_SYSTEM:
		return errors.New("type cannot be system")
	default:
		return errors.New("invalid type")
	}
	return nil
}

func (o *Organization) ValidatePatch() error {
	if o.IsEmpty() {
		return errors.New("no fields to update")
	}
	if err := RejectImmutableKeys(o); err != nil {
		return err
	}
	if !hasAnyJSONKey(o) {
		return errors.New("no fields to update")
	}
	if o.HasKey("name") && o.Name == "" {
		return errors.New("name is required")
	}
	if o.HasKey("type") {
		switch o.Type {
		case ORGANIZATION_TYPE_COMPANY, ORGANIZATION_TYPE_CHILD:
		case ORGANIZATION_TYPE_SYSTEM:
			return errors.New("type cannot be system")
		default:
			return errors.New("invalid type")
		}
	}
	return nil
}

func (u *SamnaUser) ValidatePut() error {
	if err := RejectImmutableKeys(u); err != nil {
		return err
	}
	if u.Email == "" {
		return errors.New("email is required")
	}
	if _, err := mail.ParseAddress(u.Email); err != nil {
		return errors.New("invalid email")
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

func (u *SamnaUser) ValidatePatch() error {
	if u.IsEmpty() {
		return errors.New("no fields to update")
	}
	if err := RejectImmutableKeys(u); err != nil {
		return err
	}
	if !hasAnyJSONKey(u) {
		return errors.New("no fields to update")
	}
	if u.HasKey("email") {
		if u.Email == "" {
			return errors.New("email is required")
		}
		if _, err := mail.ParseAddress(u.Email); err != nil {
			return errors.New("invalid email")
		}
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

func (c *Claim) ValidatePut() error {
	if c.Name == "" && c.Description == nil && c.Access == 0 && c.Type == "" {
		return errors.New("no fields to update")
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

func (c *Claim) ValidatePatch() error {
	if c.IsEmpty() {
		return errors.New("no fields to update")
	}
	if err := RejectImmutableKeys(c); err != nil {
		return err
	}
	if !hasAnyJSONKey(c) {
		return errors.New("no fields to update")
	}
	if c.HasKey("name") && c.Name == "" {
		return errors.New("name is required")
	}
	if c.HasKey("type") {
		switch c.Type {
		case CLAIM_TYPE_USER, CLAIM_TYPE_SERVICE, CLAIM_TYPE_VIRTUAL:
		case CLAIM_TYPE_SYSTEM:
			return errors.New("type cannot be system")
		default:
			return errors.New("invalid type")
		}
	}
	return nil
}

func (co *ClaimObject) ValidatePut() error {
	if (co.ObjectID == nil || *co.ObjectID == uuid.Nil) && (co.ObjectType == nil || *co.ObjectType == "") && co.Scope == nil && co.Reason == nil && co.Access == nil {
		return errors.New("no fields to update")
	}
	return nil
}

func (uc *UserClaim) ValidatePut() error {
	if uc.Reason == nil && uc.StartsAt == nil && uc.EndsAt == nil {
		return errors.New("no fields to update")
	}
	return nil
}

func (b *BodySecret) ValidatePut() error {
	if b.Key == nil && b.Value == nil {
		return errors.New("no fields to update")
	}
	if b.Key != nil && *b.Key == "" {
		return errors.New("key cannot be empty")
	}
	if b.Value != nil && *b.Value == "" {
		return errors.New("value cannot be empty")
	}
	return nil
}

func (f *ObjectField) ValidatePut() error {
	if f.Name == "" && f.Description == nil && f.Value == nil && f.JsonSchema == nil {
		return errors.New("no fields to update")
	}
	return nil
}

func (f *ObjectField) ValidatePatch() error {
	if f.IsEmpty() {
		return errors.New("no fields to update")
	}
	if err := RejectImmutableKeys(f); err != nil {
		return err
	}
	if !hasAnyJSONKey(f) {
		return errors.New("no fields to update")
	}
	if f.HasKey("name") && f.Name == "" {
		return errors.New("name is required")
	}
	if f.HasKey("object_type") && f.ObjectType == "" {
		return errors.New("object_type is required")
	}
	return nil
}

func (f *UserField) ValidatePut() error {
	if f.Value == nil {
		return errors.New("no fields to update")
	}
	return nil
}

func (f *UserField) ValidatePatch() error {
	if f.IsEmpty() {
		return errors.New("no fields to update")
	}
	if err := RejectImmutableKeys(f, "field_id"); err != nil {
		return err
	}
	if !hasAnyJSONKey(f) {
		return errors.New("no fields to update")
	}
	return nil
}

func (r *UserRelation) ValidatePut() error {
	if r.Description == nil && r.ObjectType == "" && r.ObjectID == uuid.Nil && r.Pinned == nil && r.Priority == nil {
		return errors.New("no fields to update")
	}
	return nil
}

func (r *UserRelation) ValidatePatch() error {
	if r.IsEmpty() {
		return errors.New("no fields to update")
	}
	if err := RejectImmutableKeys(r); err != nil {
		return err
	}
	if !hasAnyJSONKey(r) {
		return errors.New("no fields to update")
	}
	if r.HasKey("object_type") && r.ObjectType == "" {
		return errors.New("object_type is required")
	}
	if r.HasKey("object_id") && r.ObjectID == uuid.Nil {
		return errors.New("object_id is required")
	}
	return nil
}

func (a *Asset) ValidatePut() error {
	if a.Name == "" && a.Description == nil && a.MimeType == "" && a.Status == "" {
		return errors.New("no fields to update")
	}
	return nil
}

func (a *Asset) ValidatePatch() error {
	if a.IsEmpty() {
		return errors.New("no fields to update")
	}
	if err := RejectImmutableKeys(a); err != nil {
		return err
	}
	if !hasAnyJSONKey(a) {
		return errors.New("no fields to update")
	}
	if a.HasKey("name") && a.Name == "" {
		return errors.New("name is required")
	}
	if a.HasKey("mime_type") && a.MimeType == "" {
		return errors.New("mime_type is required")
	}
	return nil
}
