package echo

import (
	"errors"
	"net/mail"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func (u *SamnaUser) Bind(ctx echo.Context) error     { return u.Body.Bound(ctx, u) }
func (o *Organization) Bind(ctx echo.Context) error  { return o.Body.Bound(ctx, o) }
func (l *Location) Bind(ctx echo.Context) error      { return l.Body.Bound(ctx, l) }
func (c *Claim) Bind(ctx echo.Context) error         { return c.Body.Bound(ctx, c) }
func (uc *UserClaim) Bind(ctx echo.Context) error    { return uc.Body.Bound(ctx, uc) }
func (co *ClaimObject) Bind(ctx echo.Context) error  { return co.Body.Bound(ctx, co) }
func (sc *SamnaClient) Bind(ctx echo.Context) error  { return sc.Body.Bound(ctx, sc) }
func (ss *SamnaSecret) Bind(ctx echo.Context) error  { return ss.Body.Bound(ctx, ss) }
func (ur *UserRelation) Bind(ctx echo.Context) error { return ur.Body.Bound(ctx, ur) }
func (a *Asset) Bind(ctx echo.Context) error         { return a.Body.Bound(ctx, a) }
func (of *ObjectField) Bind(ctx echo.Context) error  { return of.Body.Bound(ctx, of) }
func (uf *UserField) Bind(ctx echo.Context) error    { return uf.Body.Bound(ctx, uf) }

var commonForbiddenKeys = []string{
	"id", "app_id", "created_by", "created_at", "updated_at", "deleted_at",
}

func disallowKeys(b interface{ HasKey(string) bool }, keys ...string) error {
	for _, k := range keys {
		if b.HasKey(k) {
			return errors.New(k + " not allowed")
		}
	}
	return nil
}

func (o *Organization) ValidatePost() error {
	if o.Name == "" {
		return errors.New("name required")
	}
	if o.OwnerID == nil || *o.OwnerID == uuid.Nil {
		return errors.New("owner_id required")
	}
	return nil
}

func (o *Organization) ValidatePut() error {
	if err := disallowKeys(o, append(commonForbiddenKeys, "owner_id", "root_id")...); err != nil {
		return err
	}
	if o.HasKey("name") && o.Name == "" {
		return errors.New("name cannot be empty")
	}
	return nil
}

func (o *Organization) ValidatePatch() error {
	if o.IsEmpty() {
		return errors.New("no fields to update")
	}
	if err := disallowKeys(o, append(commonForbiddenKeys, "owner_id", "root_id")...); err != nil {
		return err
	}
	if o.HasKey("name") && o.Name == "" {
		return errors.New("name cannot be empty")
	}
	return nil
}

func (l *Location) ValidatePost() error {
	if l.Name == "" {
		return errors.New("name required")
	}
	if l.OwnerID == uuid.Nil {
		return errors.New("owner_id required")
	}
	return nil
}

func (l *Location) ValidatePut() error {
	if err := disallowKeys(l, append(commonForbiddenKeys, "owner_id", "root_id")...); err != nil {
		return err
	}
	if l.HasKey("name") && l.Name == "" {
		return errors.New("name cannot be empty")
	}
	return nil
}

func (l *Location) ValidatePatch() error {
	if l.IsEmpty() {
		return errors.New("no fields to update")
	}
	if err := disallowKeys(l, append(commonForbiddenKeys, "owner_id", "root_id")...); err != nil {
		return err
	}
	if l.HasKey("name") && l.Name == "" {
		return errors.New("name cannot be empty")
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
	case "", CLAIM_TYPE_USER, CLAIM_TYPE_EXTERNAL:
	case CLAIM_TYPE_SYSTEM:
		return errors.New("type system not allowed")
	default:
		return errors.New("type invalid")
	}
	return nil
}

func (c *Claim) ValidatePut() error {
	if err := disallowKeys(c, append(commonForbiddenKeys, "owner_id", "root_id")...); err != nil {
		return err
	}
	if c.HasKey("name") && c.Name == "" {
		return errors.New("name cannot be empty")
	}
	return nil
}

func (c *Claim) ValidatePatch() error {
	if c.IsEmpty() {
		return errors.New("no fields to update")
	}
	if err := disallowKeys(c, append(commonForbiddenKeys, "owner_id", "root_id")...); err != nil {
		return err
	}
	if c.HasKey("name") && c.Name == "" {
		return errors.New("name cannot be empty")
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

func (co *ClaimObject) ValidatePut() error {
	if err := disallowKeys(co, append(commonForbiddenKeys, "claim_id", "owner_id", "root_id")...); err != nil {
		return err
	}
	return nil
}

func (co *ClaimObject) ValidatePatch() error {
	if co.IsEmpty() {
		return errors.New("no fields to update")
	}
	if err := disallowKeys(co, append(commonForbiddenKeys, "claim_id", "owner_id", "root_id")...); err != nil {
		return err
	}
	return nil
}

func (uc *UserClaim) ValidatePost() error {
	if uc.UserID == uuid.Nil {
		return errors.New("user_id required")
	}
	return nil
}

func (uc *UserClaim) ValidatePut() error {
	if err := disallowKeys(uc, append(commonForbiddenKeys, "claim_id", "user_id", "owner_id")...); err != nil {
		return err
	}
	return nil
}

func (uc *UserClaim) ValidatePatch() error {
	if uc.IsEmpty() {
		return errors.New("no fields to update")
	}
	if err := disallowKeys(uc, append(commonForbiddenKeys, "claim_id", "user_id", "owner_id")...); err != nil {
		return err
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
	if u.Status != "" {
		switch u.Status {
		case "active", "pending", "suspended", "inactive":
		default:
			return errors.New("status invalid")
		}
	}
	return nil
}

func (u *SamnaUser) ValidatePut() error {
	if err := disallowKeys(u, append(commonForbiddenKeys, "user_id")...); err != nil {
		return err
	}
	if u.HasKey("email") {
		if u.Email == "" {
			return errors.New("email cannot be empty")
		}
		if _, err := mail.ParseAddress(u.Email); err != nil {
			return errors.New("email invalid")
		}
	}
	return nil
}

func (u *SamnaUser) ValidatePatch() error {
	if u.IsEmpty() {
		return errors.New("no fields to update")
	}
	return u.ValidatePut()
}

func (sc *SamnaClient) ValidatePost() error {
	if sc.Name == "" {
		return errors.New("name required")
	}
	if sc.OwnerID == uuid.Nil {
		return errors.New("owner_id required")
	}
	return nil
}

func (sc *SamnaClient) ValidatePut() error {
	if err := disallowKeys(sc, append(commonForbiddenKeys, "client_id", "secret_hash", "owner_id")...); err != nil {
		return err
	}
	if sc.HasKey("name") && sc.Name == "" {
		return errors.New("name cannot be empty")
	}
	return nil
}

func (sc *SamnaClient) ValidatePatch() error {
	if sc.IsEmpty() {
		return errors.New("no fields to update")
	}
	return sc.ValidatePut()
}

func (ss *SamnaSecret) ValidatePost() error {
	if ss.Key == "" {
		return errors.New("key required")
	}
	if ss.Value == "" {
		return errors.New("value required")
	}
	if ss.OwnerID == uuid.Nil {
		return errors.New("owner_id required")
	}
	return nil
}

func (ss *SamnaSecret) ValidatePut() error {
	if err := disallowKeys(ss, append(commonForbiddenKeys, "owner_id")...); err != nil {
		return err
	}
	if ss.HasKey("key") && ss.Key == "" {
		return errors.New("key cannot be empty")
	}
	if ss.HasKey("value") && ss.Value == "" {
		return errors.New("value cannot be empty")
	}
	return nil
}

func (ss *SamnaSecret) ValidatePatch() error {
	if ss.IsEmpty() {
		return errors.New("no fields to update")
	}
	return ss.ValidatePut()
}

func (ur *UserRelation) ValidatePost() error {
	if ur.ObjectType == "" {
		return errors.New("object_type required")
	}
	if ur.ObjectID == uuid.Nil {
		return errors.New("object_id required")
	}
	return nil
}

func (ur *UserRelation) ValidatePut() error {
	if err := disallowKeys(ur, append(commonForbiddenKeys, "user_id")...); err != nil {
		return err
	}
	return nil
}

func (ur *UserRelation) ValidatePatch() error {
	if ur.IsEmpty() {
		return errors.New("no fields to update")
	}
	return ur.ValidatePut()
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

func (a *Asset) ValidatePut() error {
	if err := disallowKeys(a, append(commonForbiddenKeys, "owner_id", "blob_url", "status")...); err != nil {
		return err
	}
	if a.HasKey("name") && a.Name == "" {
		return errors.New("name cannot be empty")
	}
	return nil
}

func (a *Asset) ValidatePatch() error {
	if a.IsEmpty() {
		return errors.New("no fields to update")
	}
	return a.ValidatePut()
}

func (of *ObjectField) ValidatePost() error {
	if of.Name == "" {
		return errors.New("name required")
	}
	if of.ObjectType == "" {
		return errors.New("object_type required")
	}
	if of.OwnerID == uuid.Nil {
		return errors.New("owner_id required")
	}
	return nil
}

func (of *ObjectField) ValidatePut() error {
	if err := disallowKeys(of, append(commonForbiddenKeys, "owner_id", "object_type")...); err != nil {
		return err
	}
	if of.HasKey("name") && of.Name == "" {
		return errors.New("name cannot be empty")
	}
	return nil
}

func (of *ObjectField) ValidatePatch() error {
	if of.IsEmpty() {
		return errors.New("no fields to update")
	}
	return of.ValidatePut()
}

func (uf *UserField) ValidatePost() error {
	if uf.FieldID == uuid.Nil {
		return errors.New("field_id required")
	}
	if uf.UserID == uuid.Nil {
		return errors.New("user_id required")
	}
	return nil
}

func (uf *UserField) ValidatePut() error {
	if err := disallowKeys(uf, append(commonForbiddenKeys, "field_id", "user_id", "owner_id")...); err != nil {
		return err
	}
	return nil
}

func (uf *UserField) ValidatePatch() error {
	if uf.IsEmpty() {
		return errors.New("no fields to update")
	}
	return uf.ValidatePut()
}
