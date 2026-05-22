package db

import (
	"errors"
	"net/mail"

	"github.com/google/uuid"
)

func (l *Location) ValidatePost() error {
	if l.OwnerID == uuid.Nil {
		return errors.New("owner_id required")
	}
	return nil
}

func (o *Organization) ValidatePost() error {
	if o.Name == "" {
		return errors.New("name required")
	}
	switch o.Type {
	case "", ORGANIZATION_TYPE_COMPANY, ORGANIZATION_TYPE_CHILD:
	case ORGANIZATION_TYPE_SYSTEM:
		return errors.New("type system not allowed")
	default:
		return errors.New("type invalid")
	}
	return nil
}

func (u *SamnaUser) ValidatePost() error {
	if u.Email == "" {
		return errors.New("email required")
	}
	if _, err := mail.ParseAddress(u.Email); err != nil {
		return errors.New("email invalid")
	}
	if u.Status != nil {
		switch UserStatus(*u.Status) {
		case USER_STATUS_ACTIVE, USER_STATUS_PENDING, USER_STATUS_SUSPENDED, USER_STATUS_INACTIVE:
		default:
			return errors.New("status invalid")
		}
	}
	return nil
}

func (t *SamnaUserToken) ValidatePost() error {
	if t.Token == "" {
		return errors.New("token required")
	}
	if t.User != nil && t.User.ExternalID != nil && *t.User.ExternalID != "" {
		return errors.New("user.external_id not allowed")
	}
	return nil
}

func (c *Claim) ValidatePost() error {
	if c.Name == "" {
		return errors.New("name required")
	}
	if c.OwnerID == uuid.Nil {
		return errors.New("owner_id required")
	}
	if c.Access == 0 {
		return errors.New("access required")
	}
	switch c.Type {
	case "", CLAIM_TYPE_USER, CLAIM_TYPE_SERVICE, CLAIM_TYPE_VIRTUAL:
	case CLAIM_TYPE_SYSTEM:
		return errors.New("type system not allowed")
	default:
		return errors.New("type invalid")
	}
	return nil
}

func (co *ClaimObject) ValidatePost() error {
	if co.ObjectID == nil || *co.ObjectID == uuid.Nil {
		return errors.New("object_id required")
	}
	if co.ObjectType == nil || *co.ObjectType == "" {
		return errors.New("object_type required")
	}
	return nil
}

func (uc *UserClaim) ValidatePost() error {
	if uc.UserID == uuid.Nil {
		return errors.New("user_id required")
	}
	return nil
}

func (s *SamnaSecret) ValidatePost() error {
	if s.Key == "" {
		return errors.New("key required")
	}
	if s.Value == "" {
		return errors.New("value required")
	}
	if s.OwnerID == uuid.Nil {
		return errors.New("owner_id required")
	}
	return nil
}

func (s *SamnaSecret) ValidatePut() error {
	if s.HasKey("id") {
		return errors.New("id not allowed")
	}
	if s.HasKey("app_id") {
		return errors.New("app_id not allowed")
	}
	if s.HasKey("owner_id") {
		return errors.New("owner_id not allowed")
	}
	if s.HasKey("created_by") {
		return errors.New("created_by not allowed")
	}
	if s.HasKey("created_at") {
		return errors.New("created_at not allowed")
	}
	if s.HasKey("updated_at") {
		return errors.New("updated_at not allowed")
	}
	if s.HasKey("deleted_at") {
		return errors.New("deleted_at not allowed")
	}
	if s.Key == "" {
		return errors.New("key required")
	}
	if s.Value == "" {
		return errors.New("value required")
	}
	return nil
}

func (s *SamnaSecret) ValidatePatch() error {
	if s.IsEmpty() {
		return errors.New("no fields to update")
	}
	if s.HasKey("id") {
		return errors.New("id not allowed")
	}
	if s.HasKey("app_id") {
		return errors.New("app_id not allowed")
	}
	if s.HasKey("owner_id") {
		return errors.New("owner_id not allowed")
	}
	if s.HasKey("created_by") {
		return errors.New("created_by not allowed")
	}
	if s.HasKey("created_at") {
		return errors.New("created_at not allowed")
	}
	if s.HasKey("updated_at") {
		return errors.New("updated_at not allowed")
	}
	if s.HasKey("deleted_at") {
		return errors.New("deleted_at not allowed")
	}
	if s.HasKey("key") && s.Key == "" {
		return errors.New("key required")
	}
	if s.HasKey("value") && s.Value == "" {
		return errors.New("value required")
	}
	return nil
}

func (r *UserRelation) ValidatePost() error {
	if r.ObjectType == "" {
		return errors.New("object_type required")
	}
	if r.ObjectID == uuid.Nil {
		return errors.New("object_id required")
	}
	return nil
}

func (a *Asset) ValidatePost() error {
	if a.Name == "" {
		return errors.New("name required")
	}
	if a.MimeType == "" {
		return errors.New("mime_type required")
	}
	return nil
}

func (l *Location) ValidatePut() error {
	if l.HasKey("id") {
		return errors.New("id not allowed")
	}
	if l.HasKey("app_id") {
		return errors.New("app_id not allowed")
	}
	if l.HasKey("owner_id") {
		return errors.New("owner_id not allowed")
	}
	if l.HasKey("parent_id") {
		return errors.New("parent_id not allowed")
	}
	if l.HasKey("root_id") {
		return errors.New("root_id not allowed")
	}
	if l.HasKey("created_by") {
		return errors.New("created_by not allowed")
	}
	if l.HasKey("created_at") {
		return errors.New("created_at not allowed")
	}
	if l.HasKey("updated_at") {
		return errors.New("updated_at not allowed")
	}
	if l.HasKey("deleted_at") {
		return errors.New("deleted_at not allowed")
	}
	if l.Name == nil || *l.Name == "" {
		return errors.New("name required")
	}
	if l.Type == "" {
		return errors.New("type required")
	}
	return nil
}

func (l *Location) ValidatePatch() error {
	if l.IsEmpty() {
		return errors.New("no fields to update")
	}
	if l.HasKey("id") {
		return errors.New("id not allowed")
	}
	if l.HasKey("app_id") {
		return errors.New("app_id not allowed")
	}
	if l.HasKey("owner_id") {
		return errors.New("owner_id not allowed")
	}
	if l.HasKey("parent_id") {
		return errors.New("parent_id not allowed")
	}
	if l.HasKey("root_id") {
		return errors.New("root_id not allowed")
	}
	if l.HasKey("created_by") {
		return errors.New("created_by not allowed")
	}
	if l.HasKey("created_at") {
		return errors.New("created_at not allowed")
	}
	if l.HasKey("updated_at") {
		return errors.New("updated_at not allowed")
	}
	if l.HasKey("deleted_at") {
		return errors.New("deleted_at not allowed")
	}
	if l.HasKey("name") && (l.Name == nil || *l.Name == "") {
		return errors.New("name required")
	}
	return nil
}

func (o *Organization) ValidatePut() error {
	if o.HasKey("id") {
		return errors.New("id not allowed")
	}
	if o.HasKey("app_id") {
		return errors.New("app_id not allowed")
	}
	if o.HasKey("owner_id") {
		return errors.New("owner_id not allowed")
	}
	if o.HasKey("root_id") {
		return errors.New("root_id not allowed")
	}
	if o.HasKey("created_by") {
		return errors.New("created_by not allowed")
	}
	if o.HasKey("created_at") {
		return errors.New("created_at not allowed")
	}
	if o.HasKey("updated_at") {
		return errors.New("updated_at not allowed")
	}
	if o.HasKey("deleted_at") {
		return errors.New("deleted_at not allowed")
	}
	if o.Name == "" {
		return errors.New("name required")
	}
	switch o.Type {
	case ORGANIZATION_TYPE_COMPANY, ORGANIZATION_TYPE_CHILD:
	case "":
		return errors.New("type required")
	case ORGANIZATION_TYPE_SYSTEM:
		return errors.New("type system not allowed")
	default:
		return errors.New("type invalid")
	}
	return nil
}

func (o *Organization) ValidatePatch() error {
	if o.IsEmpty() {
		return errors.New("no fields to update")
	}
	if o.HasKey("id") {
		return errors.New("id not allowed")
	}
	if o.HasKey("app_id") {
		return errors.New("app_id not allowed")
	}
	if o.HasKey("owner_id") {
		return errors.New("owner_id not allowed")
	}
	if o.HasKey("root_id") {
		return errors.New("root_id not allowed")
	}
	if o.HasKey("created_by") {
		return errors.New("created_by not allowed")
	}
	if o.HasKey("created_at") {
		return errors.New("created_at not allowed")
	}
	if o.HasKey("updated_at") {
		return errors.New("updated_at not allowed")
	}
	if o.HasKey("deleted_at") {
		return errors.New("deleted_at not allowed")
	}
	if o.HasKey("name") && o.Name == "" {
		return errors.New("name required")
	}
	if o.HasKey("type") {
		switch o.Type {
		case ORGANIZATION_TYPE_COMPANY, ORGANIZATION_TYPE_CHILD:
		case ORGANIZATION_TYPE_SYSTEM:
			return errors.New("type system not allowed")
		default:
			return errors.New("type invalid")
		}
	}
	return nil
}

func (u *SamnaUser) ValidatePut() error {
	if u.HasKey("id") {
		return errors.New("id not allowed")
	}
	if u.HasKey("app_id") {
		return errors.New("app_id not allowed")
	}
	if u.HasKey("user_id") {
		return errors.New("user_id not allowed")
	}
	if u.HasKey("owner_id") {
		return errors.New("owner_id not allowed")
	}
	if u.HasKey("root_id") {
		return errors.New("root_id not allowed")
	}
	if u.HasKey("created_by") {
		return errors.New("created_by not allowed")
	}
	if u.HasKey("created_at") {
		return errors.New("created_at not allowed")
	}
	if u.HasKey("updated_at") {
		return errors.New("updated_at not allowed")
	}
	if u.HasKey("deleted_at") {
		return errors.New("deleted_at not allowed")
	}
	if u.Email == "" {
		return errors.New("email required")
	}
	if _, err := mail.ParseAddress(u.Email); err != nil {
		return errors.New("email invalid")
	}
	if u.Status != nil {
		switch UserStatus(*u.Status) {
		case USER_STATUS_ACTIVE, USER_STATUS_PENDING, USER_STATUS_SUSPENDED, USER_STATUS_INACTIVE:
		default:
			return errors.New("status invalid")
		}
	}
	return nil
}

func (u *SamnaUser) ValidatePatch() error {
	if u.IsEmpty() {
		return errors.New("no fields to update")
	}
	if u.HasKey("id") {
		return errors.New("id not allowed")
	}
	if u.HasKey("app_id") {
		return errors.New("app_id not allowed")
	}
	if u.HasKey("user_id") {
		return errors.New("user_id not allowed")
	}
	if u.HasKey("owner_id") {
		return errors.New("owner_id not allowed")
	}
	if u.HasKey("root_id") {
		return errors.New("root_id not allowed")
	}
	if u.HasKey("created_by") {
		return errors.New("created_by not allowed")
	}
	if u.HasKey("created_at") {
		return errors.New("created_at not allowed")
	}
	if u.HasKey("updated_at") {
		return errors.New("updated_at not allowed")
	}
	if u.HasKey("deleted_at") {
		return errors.New("deleted_at not allowed")
	}
	if u.HasKey("email") {
		if u.Email == "" {
			return errors.New("email required")
		}
		if _, err := mail.ParseAddress(u.Email); err != nil {
			return errors.New("email invalid")
		}
	}
	if u.Status != nil {
		switch UserStatus(*u.Status) {
		case USER_STATUS_ACTIVE, USER_STATUS_PENDING, USER_STATUS_SUSPENDED, USER_STATUS_INACTIVE:
		default:
			return errors.New("status invalid")
		}
	}
	return nil
}

func (c *Claim) ValidatePut() error {
	if c.HasKey("id") {
		return errors.New("id not allowed")
	}
	if c.HasKey("app_id") {
		return errors.New("app_id not allowed")
	}
	if c.HasKey("owner_id") {
		return errors.New("owner_id not allowed")
	}
	if c.HasKey("root_id") {
		return errors.New("root_id not allowed")
	}
	if c.HasKey("created_by") {
		return errors.New("created_by not allowed")
	}
	if c.HasKey("created_at") {
		return errors.New("created_at not allowed")
	}
	if c.HasKey("updated_at") {
		return errors.New("updated_at not allowed")
	}
	if c.HasKey("deleted_at") {
		return errors.New("deleted_at not allowed")
	}
	if c.Name == "" {
		return errors.New("name required")
	}
	switch c.Type {
	case CLAIM_TYPE_USER, CLAIM_TYPE_SERVICE, CLAIM_TYPE_VIRTUAL:
	case "":
		return errors.New("type required")
	case CLAIM_TYPE_SYSTEM:
		return errors.New("type system not allowed")
	default:
		return errors.New("type invalid")
	}
	return nil
}

func (c *Claim) ValidatePatch() error {
	if c.IsEmpty() {
		return errors.New("no fields to update")
	}
	if c.HasKey("id") {
		return errors.New("id not allowed")
	}
	if c.HasKey("app_id") {
		return errors.New("app_id not allowed")
	}
	if c.HasKey("owner_id") {
		return errors.New("owner_id not allowed")
	}
	if c.HasKey("root_id") {
		return errors.New("root_id not allowed")
	}
	if c.HasKey("created_by") {
		return errors.New("created_by not allowed")
	}
	if c.HasKey("created_at") {
		return errors.New("created_at not allowed")
	}
	if c.HasKey("updated_at") {
		return errors.New("updated_at not allowed")
	}
	if c.HasKey("deleted_at") {
		return errors.New("deleted_at not allowed")
	}
	if c.HasKey("name") && c.Name == "" {
		return errors.New("name required")
	}
	if c.HasKey("type") {
		switch c.Type {
		case CLAIM_TYPE_USER, CLAIM_TYPE_SERVICE, CLAIM_TYPE_VIRTUAL:
		case CLAIM_TYPE_SYSTEM:
			return errors.New("type system not allowed")
		default:
			return errors.New("type invalid")
		}
	}
	return nil
}

func (r *UserRelation) ValidatePut() error {
	if r.HasKey("id") {
		return errors.New("id not allowed")
	}
	if r.HasKey("app_id") {
		return errors.New("app_id not allowed")
	}
	if r.HasKey("user_id") {
		return errors.New("user_id not allowed")
	}
	if r.HasKey("owner_id") {
		return errors.New("owner_id not allowed")
	}
	if r.HasKey("created_by") {
		return errors.New("created_by not allowed")
	}
	if r.HasKey("created_at") {
		return errors.New("created_at not allowed")
	}
	if r.HasKey("updated_at") {
		return errors.New("updated_at not allowed")
	}
	if r.HasKey("deleted_at") {
		return errors.New("deleted_at not allowed")
	}
	if r.ObjectType == "" {
		return errors.New("object_type required")
	}
	if r.ObjectID == uuid.Nil {
		return errors.New("object_id required")
	}
	return nil
}

func (r *UserRelation) ValidatePatch() error {
	if r.IsEmpty() {
		return errors.New("no fields to update")
	}
	if r.HasKey("id") {
		return errors.New("id not allowed")
	}
	if r.HasKey("app_id") {
		return errors.New("app_id not allowed")
	}
	if r.HasKey("user_id") {
		return errors.New("user_id not allowed")
	}
	if r.HasKey("owner_id") {
		return errors.New("owner_id not allowed")
	}
	if r.HasKey("created_by") {
		return errors.New("created_by not allowed")
	}
	if r.HasKey("created_at") {
		return errors.New("created_at not allowed")
	}
	if r.HasKey("updated_at") {
		return errors.New("updated_at not allowed")
	}
	if r.HasKey("deleted_at") {
		return errors.New("deleted_at not allowed")
	}
	if r.HasKey("object_type") && r.ObjectType == "" {
		return errors.New("object_type required")
	}
	if r.HasKey("object_id") && r.ObjectID == uuid.Nil {
		return errors.New("object_id required")
	}
	return nil
}

func (a *Asset) ValidatePut() error {
	if a.HasKey("id") {
		return errors.New("id not allowed")
	}
	if a.HasKey("app_id") {
		return errors.New("app_id not allowed")
	}
	if a.HasKey("user_id") {
		return errors.New("user_id not allowed")
	}
	if a.HasKey("owner_id") {
		return errors.New("owner_id not allowed")
	}
	if a.HasKey("created_by") {
		return errors.New("created_by not allowed")
	}
	if a.HasKey("created_at") {
		return errors.New("created_at not allowed")
	}
	if a.HasKey("updated_at") {
		return errors.New("updated_at not allowed")
	}
	if a.HasKey("deleted_at") {
		return errors.New("deleted_at not allowed")
	}
	if a.Name == "" {
		return errors.New("name required")
	}
	if a.MimeType == "" {
		return errors.New("mime_type required")
	}
	return nil
}

func (a *Asset) ValidatePatch() error {
	if a.IsEmpty() {
		return errors.New("no fields to update")
	}
	if a.HasKey("id") {
		return errors.New("id not allowed")
	}
	if a.HasKey("app_id") {
		return errors.New("app_id not allowed")
	}
	if a.HasKey("user_id") {
		return errors.New("user_id not allowed")
	}
	if a.HasKey("owner_id") {
		return errors.New("owner_id not allowed")
	}
	if a.HasKey("created_by") {
		return errors.New("created_by not allowed")
	}
	if a.HasKey("created_at") {
		return errors.New("created_at not allowed")
	}
	if a.HasKey("updated_at") {
		return errors.New("updated_at not allowed")
	}
	if a.HasKey("deleted_at") {
		return errors.New("deleted_at not allowed")
	}
	if a.HasKey("name") && a.Name == "" {
		return errors.New("name required")
	}
	if a.HasKey("mime_type") && a.MimeType == "" {
		return errors.New("mime_type required")
	}
	return nil
}
