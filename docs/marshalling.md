# Marshalling

## Intended design

Marshalling and unmarshalling belong on the models themselves, not on a free standing helper like `shared.BoundBody`.

Each model exposes its own marshal and unmarshal behaviour. That behaviour lives in `shared` so the implementation is written once, and the `prophet` and `disciple` packages re-export it so a caller in either role reaches it through its own role package and never imports `shared` directly. The same pattern the rest of the role packages follow.

For a write that proxies (the disciple terminal), the body sent to the prophet is the bound body of the model (the fields the caller actually set on a patch), produced by the model, not by a `shared` function the terminal calls by name.

## Current state

The disciple update terminal currently calls `shared.BoundBody(model)` to obtain the bound patch body, and the insert and update terminals call `sauth.Marshal`. This is a stopgap kept because the sync path needs to work before this is made correct.

## What to change

1. Move the bound body and the marshal and unmarshal logic onto the model types.
2. Re-export the model surface from `prophet` and `disciple` so the terminals call the model, not `shared.BoundBody` or `sauth.Marshal` by name.
3. Remove `shared/bound.go` and the direct `sauth` calls from the role terminals once the model carries the behaviour.
