# Marshalling

Marshalling and unmarshalling live on the models. Every routed model in `shared` carries three methods, written once in `shared/marshal.go` and reached through the role packages, so a caller never imports `shared`:

| Method | Returns | Used for |
|--------|---------|----------|
| `MarshalWrite() ([]byte, error)` | The model as the prophet reads it, through `sauth.Marshal` | The body of a routed insert, and of an update with no bound body |
| `UnmarshalWrite(raw []byte) error` | The prophet's answer read back into the model, through `sauth.Unmarshal` | The row a routed write upserts locally |
| `PatchBody() map[string]any` | The fields the caller set, from the model's validator | The body of a routed `PATCH`, so a partial update sends only what changed |

The routed write terminals in `shared/query.go` ask the model through three interfaces, `writeMarshaler`, `writeUnmarshaler` and `patchBodyer`, and refuse a model that does not implement the first two. The routed models are `SamnaUser`, `Organization`, `Location`, `Claim`, `Asset`, `SamnaSecret`, `SamnaClient`, `ObjectField`, `UserField`, `UserRelation`, `SamnaUserToken`, `SamnaUserExternal`, `UserClaim` and `ClaimObject`.

The prophet's sync snapshot writes its rows with `sauth.MarshalRows`, the same wire shape, so a snapshot row and a feed payload of the same table read the same on the disciple.
