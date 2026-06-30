package shared

import "github.com/nimling/nimbody"

func BoundBody(model any) map[string]any {
	if b, ok := model.(interface {
		Validator() *nimbody.BodyValidator
	}); ok {
		if bv := b.Validator(); bv != nil {
			return bv.Body
		}
	}
	return nil
}
