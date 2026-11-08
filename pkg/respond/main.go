package respond

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"
)

type ReturnError struct {
	Message string
}

type ReturnObject struct {
	Data       any               `json:"data"`
	Error      *ReturnError      `json:"error,omitempty"`
	RequestID  uuid.UUID         `json:"request_id"`
	Count      int               `json:"count"`
	Pagination *ReturnPagination `json:"pagination,omitempty"`
}

type Pagination struct {
	LastID     *uuid.UUID  `json:"last_id"`
	Page       int         `json:"page"`
	Limit      int         `json:"limit"`
	Order      string      `json:"order"`
	Column     string      `json:"column"`
	PrevCursor *PageCursor `json:"prev_cursor"`
	NextCursor *PageCursor `json:"next_cursor"`
}

type PageCursor struct {
	Column string    `json:"column"`
	Order  string    `json:"order"`
	ID     uuid.UUID `json:"id"`
	Limit  int       `json:"limit"`
	Page   int       `json:"page"`
}

type ReturnPagination struct {
	Page  int     `json:"page,omitempty"`
	Limit int     `json:"limit,omitempty"`
	Total int     `json:"total,omitempty"`
	Prev  *string `json:"prev,omitempty"`
	Next  *string `json:"next,omitempty"`
}

var (
	idFields       = []string{"ID", "Id", "id", "object_id", "ObjectID"}
	reservedParams = map[string]bool{
		"cursor": true,
		"limit":  true,
		"order":  true,
		"page":   true,
	}
)

const defaultPageSize = 20

func Redirect(ctx echo.Context, statusCode int, url string) error {
	return ctx.Redirect(statusCode, url)
}

func Stream(ctx echo.Context, statusCode int, contentType string, r io.Reader) error {
	return ctx.Stream(statusCode, contentType, r)
}

func ParseInclude(ctx echo.Context) map[string]bool {
	include := make(map[string]bool)
	param := ctx.QueryParam("include")
	if param == "" {
		return include
	}
	for _, item := range strings.Split(param, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			include[item] = true
		}
	}
	return include
}

func JSON(ctx echo.Context, statusCode int, data interface{}, count int) error {
	rt := reflect.TypeOf(data)

	requestID, ok := ctx.Get("request_id").(uuid.UUID)
	if !ok {
		err := errors.New("Missing request_id in context")
		panic(err)
	}

	returnData := ReturnObject{
		Count:     count,
		RequestID: requestID,
	}

	total := count
	switch rt.Kind() {
	case reflect.Slice, reflect.Array:
		returnData.Data = data
		v := reflect.ValueOf(data)
		count = v.Len()
		break
	default:
		returnData.Data = data
		count = 1
		break
	}

	returnData.WithPagination(ctx, count, total)

	ctx.Response().Header().Set(echo.HeaderContentType, echo.MIMEApplicationJSONCharsetUTF8)
	ctx.Response().WriteHeader(statusCode)
	enc := json.NewEncoder(ctx.Response())
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(returnData)
}

func Error(ctx echo.Context, statusCode int, message string) error {
	requestID, ok := ctx.Get("request_id").(uuid.UUID)
	if !ok {
		err := errors.New("Missing request_id in context")
		panic(err)
	}

	returnData := ReturnObject{
		Count: 0,
		Data:  []interface{}{},
		Error: &ReturnError{
			message,
		},
		RequestID: requestID,
	}

	ctx.Response().Header().Set(echo.HeaderContentType, echo.MIMEApplicationJSONCharsetUTF8)
	ctx.Response().WriteHeader(statusCode)
	enc := json.NewEncoder(ctx.Response())
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(returnData)
}

func NoContent(ctx echo.Context, statusCode int) error {
	requestID, ok := ctx.Get("request_id").(uuid.UUID)
	if !ok {
		err := errors.New("Missing request_id in context")
		panic(err)
	}

	returnData := ReturnObject{
		Count:     0,
		Data:      []interface{}{},
		RequestID: requestID,
	}

	ctx.Response().Header().Set(echo.HeaderContentType, echo.MIMEApplicationJSONCharsetUTF8)
	ctx.Response().WriteHeader(statusCode)
	enc := json.NewEncoder(ctx.Response())
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(returnData)
}

func InitPagination(ctx echo.Context, limit *int, column *string) error {
	params := ctx.QueryParams()

	pagination := Pagination{
		LastID: nil,
		Page:   1,
		Limit:  defaultPageSize,
		Order:  "DESC",
		Column: "id",
	}

	if column != nil {
		pagination.Column = *column
	}

	if limit != nil {
		pagination.Limit = *limit
	} else if params.Has("limit") {
		paramLimit, err := strconv.Atoi(params.Get("limit"))
		if err != nil {
			return nil
		}
		pagination.Limit = paramLimit
	}

	if params.Has("page") {
		paramPage, err := strconv.Atoi(params.Get("page"))
		if err != nil {
			return nil
		}
		pagination.Page = paramPage
	}

	if params.Has("column") {
		column := params.Get("column")
		pagination.Column = column
	}

	if params.Has("cursor") {
		cursorString := params.Get("cursor")
		if len(cursorString) > 0 {
			decodedCursor, err := base64.StdEncoding.DecodeString(cursorString)
			if err != nil {
				ctx.Logger().Warnf("invalid query params: %v", err)
				return err
			}

			var cursor PageCursor
			err = json.Unmarshal(decodedCursor, &cursor)
			if err != nil {
				ctx.Logger().Warnf("invalid query params: %v", err)
				return err
			}

			if cursor.Limit <= 0 {
				cursor.Limit = defaultPageSize
			}

			pagination.PrevCursor = &cursor
			pagination.Limit = cursor.Limit
			pagination.Page = cursor.Page
			pagination.Column = cursor.Column
			pagination.LastID = &cursor.ID
		}
	}

	if params.Has("order") {
		pagination.Order = params.Get("order")
		if pagination.Order == "asc" || pagination.Order == "ASC" {
			pagination.Order = "ASC"
		} else {
			pagination.Order = "DESC"
		}
	}

	ctx.Set("pagination", pagination)
	return nil
}

func GetPagination(ctx echo.Context) *Pagination {
	pagination, ok := ctx.Get("pagination").(Pagination)
	if !ok {
		panic("Missing pagination in context")
	}
	return &pagination
}

func (p *Pagination) Offset() int {
	if p.LastID == nil {
		return p.Limit * (p.Page - 1)
	}
	return 0
}

func PaginationQuery(ctx echo.Context) func(query *bun.SelectQuery) *bun.SelectQuery {
	pagination, ok := ctx.Get("pagination").(Pagination)
	if !ok {
		err := errors.New("missing pagination in context")
		panic(err)
	}

	return func(query *bun.SelectQuery) *bun.SelectQuery {
		if pagination.PrevCursor != nil {
			cursor := pagination.PrevCursor
			if cursor.Order == "asc" || cursor.Order == "ASC" {
				query = query.Order(cursor.Column+" ASC").Where("? > ?", cursor.Column, cursor.ID).Limit(cursor.Limit)
			} else {
				query = query.Order(cursor.Column+" DESC").Where("? < ?", cursor.Column, cursor.ID).Limit(cursor.Limit)
			}
		} else if pagination.Page > 1 {
			query = query.Order(pagination.Column + " " + pagination.Order).Limit(pagination.Limit).
				Offset((pagination.Page - 1) * pagination.Limit)
		} else {
			query = query.Order(pagination.Column + " " + pagination.Order).Limit(pagination.Limit)
		}

		return query
	}
}

func (r *ReturnObject) WithPagination(ctx echo.Context, count, total int) {
	paginationObj, ok := ctx.Get("pagination").(Pagination)
	if ok {
		returnPagination := &ReturnPagination{
			Page:  paginationObj.Page,
			Limit: paginationObj.Limit,
			Total: total,
		}

		baseURL := ctx.Request().URL.Path

		queryParams := ctx.QueryParams()
		var additionalParams string

		for key, values := range queryParams {
			if reservedParams[key] {
				continue
			}

			for _, value := range values {
				if additionalParams != "" {
					additionalParams += "&"
				}
				additionalParams += key + "=" + value
			}
		}

		var lastID uuid.UUID
		var hasLastItem bool

		dataVal := reflect.ValueOf(r.Data)
		if dataVal.Kind() == reflect.Slice && dataVal.Len() > 0 {
			lastItemVal := dataVal.Index(dataVal.Len() - 1)

			if lastItemVal.Kind() == reflect.Ptr && !lastItemVal.IsNil() {
				lastItemVal = lastItemVal.Elem()
			}

			if lastItemVal.Kind() == reflect.Struct {
				var idField reflect.Value
				for _, fieldName := range idFields {
					idField = lastItemVal.FieldByName(fieldName)
					if idField.IsValid() {
						break
					}
				}

				if idField.IsValid() {
					if uuidValue, ok := idField.Interface().(uuid.UUID); ok {
						lastID = uuidValue
						hasLastItem = true
					} else {
						var idStr string

						switch idField.Kind() {
						case reflect.String:
							idStr = idField.String()
						default:
							idStr = fmt.Sprintf("%v", idField.Interface())
						}

						parsedID, err := uuid.Parse(idStr)
						if err == nil {
							lastID = parsedID
							hasLastItem = true
						} else {
							ctx.Logger().Warnf("Failed to parse ID as UUID: %v", err)
						}
					}
				} else {
					ctx.Logger().Warn("Could not find ID field in data item")
				}
			}
		}

		if hasLastItem {
			pageCursor := PageCursor{
				Order:  "DESC",
				ID:     lastID,
				Limit:  paginationObj.Limit,
				Page:   paginationObj.Page + 1,
				Column: paginationObj.Column,
			}

			if paginationObj.PrevCursor != nil {
				pageCursor.Order = paginationObj.PrevCursor.Order
				pageCursor.Column = paginationObj.PrevCursor.Column
				cursorBytes, _ := json.Marshal(*paginationObj.PrevCursor)
				cursorStr := base64.StdEncoding.EncodeToString(cursorBytes)
				prevURL := fmt.Sprintf("%s?cursor=%s", baseURL, cursorStr)
				if additionalParams != "" {
					prevURL += "&" + additionalParams
				}
				returnPagination.Prev = &prevURL
			}

			if paginationObj.Page*paginationObj.Limit < total {
				cursorBytes, _ := json.Marshal(pageCursor)
				cursorStr := base64.StdEncoding.EncodeToString(cursorBytes)
				nextURL := fmt.Sprintf("%s?cursor=%s", baseURL, cursorStr)
				if additionalParams != "" {
					nextURL += "&" + additionalParams
				}
				returnPagination.Next = &nextURL
				returnPagination.Page = paginationObj.Page
			}
		}

		r.Count = count
		r.Pagination = returnPagination
	}
}

func (p *PageCursor) Encoded() any {
	if p == nil {
		return nil
	}
	nextCursorBytes, _ := json.Marshal(*p)
	return base64.StdEncoding.EncodeToString(nextCursorBytes)
}
