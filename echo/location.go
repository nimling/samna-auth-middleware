package echo

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/utils"
)

func GetLocations(ctx echo.Context) error {
	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.LocationRelations())
		locs, err := db.Instance().GetLocations(ctx.Request().Context(), *debugUserID, db.ACCESS_LEVEL_MEMBER, relations...)
		if err != nil {
			ctx.Logger().Errorf("GetLocations: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return RespondJSON(ctx, http.StatusOK, locs, nil)
	}

	body, err := client.GetLocations(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("GetLocations: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetLocation(ctx echo.Context) error {
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		claims, err := db.Instance().ClaimAccess(ctx.Request().Context(), *debugUserID, &locationID, int(db.ACCESS_LEVEL_MEMBER))
		if err != nil || len(claims) == 0 {
			return RespondError(ctx, http.StatusNotFound, "Not found")
		}
		relations := db.ParseWithRelations(ctx.QueryParam("with"), db.LocationRelations())
		loc, err := db.Instance().GetLocationByID(ctx.Request().Context(), locationID, relations...)
		if err != nil {
			ctx.Logger().Errorf("GetLocation: %v", err)
			return RespondError(ctx, http.StatusNotFound, "Not found")
		}
		return RespondJSON(ctx, http.StatusOK, loc, nil)
	}

	body, err := client.GetLocation(ctx.Request().Context(), locationID)
	if err != nil {
		ctx.Logger().Errorf("GetLocation: %v", err)
		return RespondError(ctx, http.StatusNotFound, "Not found")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetLocationAudit(ctx echo.Context) error {
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if utils.GetDebugUserID() != nil {
		audits, err := db.Instance().GetLocationAudit(ctx.Request().Context(), locationID)
		if err != nil {
			ctx.Logger().Errorf("GetLocationAudit: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return RespondJSON(ctx, http.StatusOK, audits, nil)
	}

	body, err := client.GetLocationAudit(ctx.Request().Context(), locationID)
	if err != nil {
		ctx.Logger().Errorf("GetLocationAudit: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func GetLocationAssets(ctx echo.Context) error {
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	body, err := client.GetLocationAssets(ctx.Request().Context(), locationID)
	if err != nil {
		ctx.Logger().Errorf("GetLocationAssets: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}

func PostLocation(ctx echo.Context) error {
	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		var loc db.Location
		if err := ctx.Bind(&loc); err != nil {
			return RespondError(ctx, http.StatusBadRequest, "Bad request")
		}
		ownerID := loc.OwnerID
		claims, err := db.Instance().ClaimAccess(ctx.Request().Context(), *debugUserID, &ownerID, int(db.ACCESS_LEVEL_WRITE))
		if err != nil {
			ctx.Logger().Errorf("PostLocation ClaimAccess: %v", err)
			return RespondError(ctx, http.StatusForbidden, "Forbidden")
		}
		if len(claims) == 0 {
			ctx.Logger().Infof("PostLocation: no access for user %s on object %s", *debugUserID, ownerID)
			return RespondError(ctx, http.StatusForbidden, "Forbidden")
		}
		loc.CreatedBy = claims[0].ID
		loc.RootID = claims[0].RootID
		if err := db.Instance().CreateLocation(ctx.Request().Context(), &loc); err != nil {
			ctx.Logger().Errorf("PostLocation: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}

		claimName := "location - " + func() string { if loc.Name != nil { return *loc.Name }; return loc.ID.String() }()
		ownerClaim, claimErr := client.PostClaim(ctx.Request().Context(), db.Claim{
			Name:        claimName,
			AccessLevel: int(db.ACCESS_LEVEL_OWNER),
			OwnerID:     loc.OwnerID,
			RootID:      loc.RootID,
			Type:        db.CLAIM_TYPE_VIRTUAL,
		})
		if claimErr != nil {
			ctx.Logger().Warnf("PostLocation: failed to create claim: %v", claimErr)
		} else {
			_, claimErr = client.PostClaimObject(ctx.Request().Context(), ownerClaim.ID, db.ClaimObject{
				ClaimID:    ownerClaim.ID,
				ObjectID:   loc.ID,
				ObjectType: "location",
				OwnerID:    loc.OwnerID,
				RootID:     loc.RootID,
				Reason:     strPtr("virtual - " + claimName),
			})
			if claimErr != nil {
				ctx.Logger().Warnf("PostLocation: failed to create claim object: %v", claimErr)
			}
			_, claimErr = client.PostUserClaim(ctx.Request().Context(), ownerClaim.ID, db.UserClaim{
				ClaimID: ownerClaim.ID,
				UserID:  *debugUserID,
				OwnerID: loc.OwnerID,
				Reason:  strPtr("virtual"),
			})
			if claimErr != nil {
				ctx.Logger().Warnf("PostLocation: failed to create user claim: %v", claimErr)
			}
		}

		return RespondJSON(ctx, http.StatusCreated, loc, nil)
	}

	var loc db.Location
	if err := ctx.Bind(&loc); err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}
	result, err := client.PostLocation(ctx.Request().Context(), loc)
	if err != nil {
		ctx.Logger().Errorf("PostLocation: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusCreated, result, nil)
}

func PutLocation(ctx echo.Context) error {
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		claims, err := db.Instance().ClaimAccess(ctx.Request().Context(), *debugUserID, &locationID, int(db.ACCESS_LEVEL_WRITE))
		if err != nil || len(claims) == 0 {
			return RespondError(ctx, http.StatusForbidden, "Forbidden")
		}
		var loc db.Location
		if err := ctx.Bind(&loc); err != nil {
			return RespondError(ctx, http.StatusBadRequest, "Bad request")
		}
		if err := db.Instance().UpdateLocation(ctx.Request().Context(), locationID, &loc); err != nil {
			ctx.Logger().Errorf("PutLocation: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return RespondJSON(ctx, http.StatusOK, loc, nil)
	}

	var loc db.Location
	if err := ctx.Bind(&loc); err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}
	result, err := client.PutLocation(ctx.Request().Context(), locationID, loc)
	if err != nil {
		ctx.Logger().Errorf("PutLocation: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusOK, result, nil)
}

func DeleteLocation(ctx echo.Context) error {
	locationID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return RespondError(ctx, http.StatusBadRequest, "Bad request")
	}

	if debugUserID := utils.GetDebugUserID(); debugUserID != nil {
		claims, err := db.Instance().ClaimAccess(ctx.Request().Context(), *debugUserID, &locationID, int(db.ACCESS_LEVEL_WRITE))
		if err != nil || len(claims) == 0 {
			return RespondError(ctx, http.StatusForbidden, "Forbidden")
		}
		if err := db.Instance().DeleteLocation(ctx.Request().Context(), locationID); err != nil {
			ctx.Logger().Errorf("DeleteLocation: %v", err)
			return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
		}
		return ctx.NoContent(http.StatusOK)
	}

	result, err := client.DeleteLocation(ctx.Request().Context(), locationID)
	if err != nil {
		ctx.Logger().Errorf("DeleteLocation: %v", err)
		return RespondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return RespondJSON(ctx, http.StatusOK, result, nil)
}
