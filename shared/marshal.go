package shared

import "github.com/nimling/samna-auth-middleware/v2/sauth"

func (m *SamnaUser) MarshalWrite() ([]byte, error)   { return sauth.Marshal(m) }
func (m *SamnaUser) UnmarshalWrite(raw []byte) error { return sauth.Unmarshal(raw, m) }
func (m *SamnaUser) PatchBody() map[string]any {
	if bv := m.Validator(); bv != nil {
		return bv.Body
	}
	return nil
}

func (m *Organization) MarshalWrite() ([]byte, error)   { return sauth.Marshal(m) }
func (m *Organization) UnmarshalWrite(raw []byte) error { return sauth.Unmarshal(raw, m) }
func (m *Organization) PatchBody() map[string]any {
	if bv := m.Validator(); bv != nil {
		return bv.Body
	}
	return nil
}

func (m *Location) MarshalWrite() ([]byte, error)   { return sauth.Marshal(m) }
func (m *Location) UnmarshalWrite(raw []byte) error { return sauth.Unmarshal(raw, m) }
func (m *Location) PatchBody() map[string]any {
	if bv := m.Validator(); bv != nil {
		return bv.Body
	}
	return nil
}

func (m *Claim) MarshalWrite() ([]byte, error)   { return sauth.Marshal(m) }
func (m *Claim) UnmarshalWrite(raw []byte) error { return sauth.Unmarshal(raw, m) }
func (m *Claim) PatchBody() map[string]any {
	if bv := m.Validator(); bv != nil {
		return bv.Body
	}
	return nil
}

func (m *Asset) MarshalWrite() ([]byte, error)   { return sauth.Marshal(m) }
func (m *Asset) UnmarshalWrite(raw []byte) error { return sauth.Unmarshal(raw, m) }
func (m *Asset) PatchBody() map[string]any {
	if bv := m.Validator(); bv != nil {
		return bv.Body
	}
	return nil
}

func (m *SamnaSecret) MarshalWrite() ([]byte, error)   { return sauth.Marshal(m) }
func (m *SamnaSecret) UnmarshalWrite(raw []byte) error { return sauth.Unmarshal(raw, m) }
func (m *SamnaSecret) PatchBody() map[string]any {
	if bv := m.Validator(); bv != nil {
		return bv.Body
	}
	return nil
}

func (m *SamnaClient) MarshalWrite() ([]byte, error)   { return sauth.Marshal(m) }
func (m *SamnaClient) UnmarshalWrite(raw []byte) error { return sauth.Unmarshal(raw, m) }
func (m *SamnaClient) PatchBody() map[string]any {
	if bv := m.Validator(); bv != nil {
		return bv.Body
	}
	return nil
}

func (m *ObjectField) MarshalWrite() ([]byte, error)   { return sauth.Marshal(m) }
func (m *ObjectField) UnmarshalWrite(raw []byte) error { return sauth.Unmarshal(raw, m) }
func (m *ObjectField) PatchBody() map[string]any {
	if bv := m.Validator(); bv != nil {
		return bv.Body
	}
	return nil
}

func (m *UserField) MarshalWrite() ([]byte, error)   { return sauth.Marshal(m) }
func (m *UserField) UnmarshalWrite(raw []byte) error { return sauth.Unmarshal(raw, m) }
func (m *UserField) PatchBody() map[string]any {
	if bv := m.Validator(); bv != nil {
		return bv.Body
	}
	return nil
}

func (m *UserRelation) MarshalWrite() ([]byte, error)   { return sauth.Marshal(m) }
func (m *UserRelation) UnmarshalWrite(raw []byte) error { return sauth.Unmarshal(raw, m) }
func (m *UserRelation) PatchBody() map[string]any {
	if bv := m.Validator(); bv != nil {
		return bv.Body
	}
	return nil
}

func (m *SamnaUserToken) MarshalWrite() ([]byte, error)   { return sauth.Marshal(m) }
func (m *SamnaUserToken) UnmarshalWrite(raw []byte) error { return sauth.Unmarshal(raw, m) }
func (m *SamnaUserToken) WriteResult() any                { return &SamnaUser{} }
func (m *SamnaUserToken) PatchBody() map[string]any {
	if bv := m.Validator(); bv != nil {
		return bv.Body
	}
	return nil
}

func (m *SamnaUserExternal) MarshalWrite() ([]byte, error)   { return sauth.Marshal(m) }
func (m *SamnaUserExternal) UnmarshalWrite(raw []byte) error { return sauth.Unmarshal(raw, m) }
func (m *SamnaUserExternal) WriteResult() any                { return &SamnaUser{} }
func (m *SamnaUserExternal) PatchBody() map[string]any {
	if bv := m.Validator(); bv != nil {
		return bv.Body
	}
	return nil
}

func (m *UserClaim) MarshalWrite() ([]byte, error)   { return sauth.Marshal(m) }
func (m *UserClaim) UnmarshalWrite(raw []byte) error { return sauth.Unmarshal(raw, m) }
func (m *UserClaim) PatchBody() map[string]any {
	if bv := m.Validator(); bv != nil {
		return bv.Body
	}
	return nil
}

func (m *ClaimObject) MarshalWrite() ([]byte, error)   { return sauth.Marshal(m) }
func (m *ClaimObject) UnmarshalWrite(raw []byte) error { return sauth.Unmarshal(raw, m) }
func (m *ClaimObject) PatchBody() map[string]any {
	if bv := m.Validator(); bv != nil {
		return bv.Body
	}
	return nil
}
